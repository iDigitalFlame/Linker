// linker.go
// URL Shortener with MySQL database.
//
// Copyright (C) 2020 iDigitalFlame
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//

package linker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	// Import for the Golang MySQL driver
	_ "github.com/go-sql-driver/mysql"
)

// DefaultConfig is a string representation of the default configuration for Linker.
// This can be used in a JSON file to configure Linker.
const DefaultConfig = `{
    "key": "",
    "cert": "",
    "listen": "0.0.0.0:80",
    "timeout": 5,
    "default": "https://duckduckgo.com",
    "db": {
        "name": "linker",
        "server": "tcp(localhost:3306)",
        "username": "linker_user",
        "password": "password"
    }
}`

const (
	sqlGet    = "SELECT LinkURL FROM Links WHERE LinkName = ?"
	sqlAdd    = "INSERT INTO Links(LinkName, LinkURL) VALUES(?, ?)"
	sqlList   = "SELECT LinkName, LinkURL FROM Links"
	sqlDelete = "DELETE FROM Links WHERE LinkName = ?"

	defaultURL      = "https://duckduckgo.com"
	defaultFile     = "/etc/linker.conf"
	defaultTimeout  = 5 * time.Second
	defaultDatabase = "CREATE TABLE IF NOT EXISTS Links (LinkID INT(32) NOT NULL PRIMARY KEY AUTO_INCREMENT, " +
		"LinkName VARCHAR(64) NOT NULL UNIQUE, LinkURL VARCHAR(1024) NOT NULL)"
)

var (
	// ErrInvalidName is an error returned by the Add or Delete functions when a name is
	// passed that contains any invalid or non printable characters.
	ErrInvalidName = errors.New("name contains invalid characters")
	// ErrNotConfigured is an error that is returned when any operations are attempted on a
	// non-loaded Linker instance.
	ErrNotConfigured = errors.New("database is not loaded or configured")

	regCheckURL = regexp.MustCompile("(^\\/[a-zA-Z0-9]+)")
)

// Linker is a struct that contains the web service and SQL queries that
// support the Linker URL shortener.
type Linker struct {
	db     *sql.DB
	ctx    context.Context
	get    *sql.Stmt
	url    string
	key    string
	cert   string
	cancel context.CancelFunc
	*http.Server
}
type config struct {
	Key      string    `json:"key"`
	Cert     string    `json:"cert"`
	Listen   string    `json:"listen"`
	Timeout  uint8     `json:"timeout"`
	Default  string    `json:"default"`
	Database *database `json:"db"`
}
type database struct {
	Name     string `json:"name"`
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// List will gather and print all the current link dataset. This function returns an error
// if there an error reading from the database.
func (l *Linker) List() error {
	if l.db == nil {
		return ErrNotConfigured
	}
	q, err := l.db.Prepare(sqlList)
	if err != nil {
		return fmt.Errorf("unable to prepare query statement: %w", err)
	}
	defer q.Close()
	r, err := q.Query()
	if err != nil {
		return fmt.Errorf("unable to execute query statement: %w", err)
	}
	defer r.Close()
	fmt.Printf("%-14s%s\n==============================================\n", "Name", "URL")
	var n, u string
	for r.Next() {
		if err := r.Scan(&n, &u); err != nil {
			return fmt.Errorf("unable to parse query statement results: %w", err)
		}
		fmt.Printf("%-14s%s\n", n, u)
	}
	return nil
}

// Close will attempt to close the connection to the database and stop any running services
// associated with the Linker struct.
func (l *Linker) Close() error {
	if l.get != nil {
		if err := l.get.Close(); err != nil {
			return fmt.Errorf("unable to close get statement: %w", err)
		}
	}
	if l.db != nil {
		if err := l.db.Close(); err != nil {
			return fmt.Errorf("unable to close database: %w", err)
		}
	}
	if l != nil && l.ctx.Err() != nil {
		l.cancel()
		if err := l.Server.Shutdown(l.ctx); err != nil {
			return fmt.Errorf("unable to shutdown server: %w", err)
		}
	}
	return l.Server.Close()
}
func isNameValid(s string) bool {
	for _, v := range s {
		if v < 48 || v > 123 {
			return false
		}
		switch v {
		case ':', ';', '<', '=', '>', '?', '@', '[', ']', '\\', '^', '_', '`', '{', '|', '}', '~':
			return false
		default:
		}
	}
	return true
}

// Listen will start the listing session for Linker to redirect HTTP requests. This function
// will block until the Close function is called or a SIGINT is received. This function will
// return an error if there is an issue during the listener creation.
func (l *Linker) Listen() error {
	if l.get != nil {
		return ErrNotConfigured
	}
	var (
		s   = make(chan os.Signal)
		err error
	)
	l.ctx, l.cancel = context.WithCancel(context.Background())
	if l.get, err = l.db.PrepareContext(l.ctx, sqlGet); err != nil {
		return fmt.Errorf("unable to prepare get statement: %w", err)
	}
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGKILL)
	go func(err *error) {
		*err = l.Server.ListenAndServe()
	}(&err)
	select {
	case <-s:
	case <-l.ctx.Done():
	}
	return err
}

// New creates a new Linker instance and attempts to gather the initial configuration from a JSON formatted file.
// The path to this file can be passed in the string argument or read from the "LINKER_CONFIG" environment variable.
// This function will return an error if the load could not happen on the configuration file is invalid.
func New(s string) (*Linker, error) {
	l := &Linker{Server: &http.Server{Handler: &http.ServeMux{}}}
	if err := l.load(s); err != nil {
		return nil, err
	}
	return l, nil
}
func (l *Linker) load(s string) error {
	var c config
	if len(s) == 0 {
		if v, ok := os.LookupEnv("LINKER_CONFIG"); ok {
			s = v
		} else {
			s = defaultFile
		}
	}
	i, err := os.Stat(s)
	if err != nil {
		return fmt.Errorf("%q: %w", s, os.ErrNotExist)
	}
	if i.IsDir() {
		return fmt.Errorf("path %q is not a file", s)
	}
	b, err := ioutil.ReadFile(s)
	if err != nil {
		return fmt.Errorf("unable to read %q: %w", s, err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return fmt.Errorf("unable to parse %q: %w", s, err)
	}
	if c.Database == nil {
		return fmt.Errorf("%q does not contain a database configuration", s)
	}
	if l.db, err = sql.Open("mysql", fmt.Sprintf("%s:%s@%s/%s", c.Database.Username, c.Database.Password, c.Database.Server, c.Database.Name)); err != nil {
		return fmt.Errorf("unable to connect to database %q on %q: %w", c.Database.Name, c.Database.Server, err)
	}
	if err := l.db.Ping(); err != nil {
		return fmt.Errorf("unable to connect to database %q on %q: %w", c.Database.Name, c.Database.Server, err)
	}
	n, err := l.db.Prepare(defaultDatabase)
	if err != nil {
		return fmt.Errorf("unable to prepare initial table in database %q on %q: %w", c.Database.Name, c.Database.Server, err)
	}
	defer n.Close()
	if _, err := n.Exec(); err != nil {
		return fmt.Errorf("unable to create initial table in database %q on %q: %w", c.Database.Name, c.Database.Server, err)
	}
	if len(c.Default) > 0 {
		u, err := url.Parse(c.Default)
		if err != nil {
			return fmt.Errorf("unable to parse default url %q: %w", c.Default, err)
		}
		if len(u.Scheme) == 0 {
			u.Scheme = "https"
		}
		l.url = u.String()
	}
	if len(l.url) == 0 {
		l.url = defaultURL
	}
	l.Server.Addr = c.Listen
	l.key, l.cert = c.Key, c.Cert
	l.Server.BaseContext = l.context
	l.Server.ReadTimeout = time.Second * time.Duration(c.Timeout)
	l.Server.IdleTimeout = l.Server.ReadTimeout
	l.Server.WriteTimeout, l.Server.ReadHeaderTimeout = l.Server.ReadTimeout, l.Server.ReadTimeout
	l.Server.Handler.(*http.ServeMux).HandleFunc("/", l.serve)
	return nil
}

// Add will attempt to add a redirect with the name of the first string to the URL provided in the
// second string argument. This function will return an error if the add fails.
func (l *Linker) Add(n, u string) error {
	if l.db == nil {
		return ErrNotConfigured
	}
	if !isNameValid(n) {
		return ErrInvalidName
	}
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", u, err)
	}
	if len(p.Scheme) == 0 {
		p.Scheme = "https"
	}
	q, err := l.db.Prepare(sqlAdd)
	if err != nil {
		return fmt.Errorf("unable to prepare add statement: %w", err)
	}
	defer q.Close()
	r, err := q.Exec(n, p.String())
	if err != nil {
		return fmt.Errorf("unable to execute add statement: %w", err)
	}
	if _, err := r.RowsAffected(); err != nil {
		return fmt.Errorf("unable to process add statement: %w", err)
	}
	return nil
}

// Delete will attempt to remove the redirect name and URL using the mapping name. This function
// will return an error if the deletion fails. This function will pass even if the URL does not exist.
func (l *Linker) Delete(n string) error {
	if l.db == nil {
		return ErrNotConfigured
	}
	if !isNameValid(n) {
		return ErrInvalidName
	}
	q, err := l.db.Prepare(sqlDelete)
	if err != nil {
		return fmt.Errorf("unable to prepare delete statement: %w", err)
	}
	defer q.Close()
	r, err := q.Exec(n)
	if err != nil {
		return fmt.Errorf("unable to execute delete statement: %w", err)
	}
	if _, err := r.RowsAffected(); err != nil {
		return fmt.Errorf("unable to process delete statement: %w", err)
	}
	return nil
}
func (l *Linker) context(_ net.Listener) context.Context {
	return l.ctx
}
func (l *Linker) serve(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("http function recovered from a panic: %s\n", err)
		}
	}()
	if len(r.RequestURI) <= 1 {
		http.Redirect(w, r, l.url, http.StatusTemporaryRedirect)
		return
	}
	s := html.EscapeString(r.RequestURI)
	p := regCheckURL.FindStringIndex(s)
	if p == nil || p[0] != 0 || p[1] <= 1 {
		http.Redirect(w, r, l.url, http.StatusTemporaryRedirect)
		return
	}
	n, x := "", s[1:p[1]]
	if err := l.get.QueryRowContext(l.ctx, x).Scan(&n); err != nil {
		if err == sql.ErrNoRows {
			http.Redirect(w, r, l.url, http.StatusTemporaryRedirect)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Could not fetch requested URL: %q", x)
			fmt.Printf("http function received an error: %s\n", err.Error())
		}
		return
	}
	if len(n) == 0 {
		http.Redirect(w, r, l.url, http.StatusTemporaryRedirect)
		return
	}
	if p[1] < len(s) {
		n = fmt.Sprintf("%s%s", n, s[p[1]:])
	}
	http.Redirect(w, r, n, http.StatusTemporaryRedirect)
}
