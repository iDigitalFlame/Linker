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
	"crypto/tls"
	"database/sql"
	"encoding/json"
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

// Defaults is a string representation of the default configuration for Linker. This can be used in a JSON file
// to configure a Linker instance.
const Defaults = `{
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
}
`

const (
	sqlGet     = `SELECT LinkURL FROM Links WHERE LinkName = ?`
	sqlAdd     = `INSERT INTO Links(LinkName, LinkURL) VALUES(?, ?)`
	sqlList    = `SELECT LinkName, LinkURL FROM Links`
	sqlDelete  = `DELETE FROM Links WHERE LinkName = ?`
	sqlPrepare = `CREATE TABLE IF NOT EXISTS Links (LinkID INT(64) NOT NULL PRIMARY KEY AUTO_INCREMENT,
		LinkName VARCHAR(64) NOT NULL UNIQUE, LinkURL VARCHAR(1024) NOT NULL)`

	defaultURL     = `https://duckduckgo.com`
	defaultFile    = `/etc/linker.conf`
	defaultTimeout = 5 * time.Second
)

var (
	regCheckURL      = regexp.MustCompile(`(^\/[a-zA-Z0-9]+)`)
	errNotConfigured = &errval{s: "database is not loaded or configured"}
)

// Linker is a struct that contains the web service and SQL queries that support the Linker URL shortener.
type Linker struct {
	db     *sql.DB
	ctx    context.Context
	get    *sql.Stmt
	url    string
	key    string
	cert   string
	cancel context.CancelFunc
	http.Server
}
type errval struct {
	e error
	s string
}
type config struct {
	Key      string   `json:"key"`
	Cert     string   `json:"cert"`
	Listen   string   `json:"listen"`
	Timeout  uint8    `json:"timeout"`
	Default  string   `json:"default"`
	Database database `json:"db"`
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
		return errNotConfigured
	}
	q, err := l.db.Prepare(sqlList)
	if err != nil {
		return &errval{s: "unable to prepare query statement", e: err}
	}
	r, err := q.Query()
	if err != nil {
		q.Close()
		return &errval{s: "unable to execute query statement", e: err}
	}
	var n, u string
	for os.Stdout.WriteString(expand("Name", 15) + "URL\n==============================================\n"); r.Next(); {
		if err = r.Scan(&n, &u); err != nil {
			break
		}
		os.Stdout.WriteString(expand(n, 15) + u + "\n")
	}
	r.Close()
	if q.Close(); err != nil {
		return &errval{s: "unable to parse query statement results", e: err}
	}
	return nil
}
func validName(s string) bool {
	for i := range s {
		switch {
		case s[i] == 45:
		case s[i] == 95:
		case s[i] > 90 && s[i] < 97:
			return false
		case s[i] > 57 && s[i] < 65:
			return false
		case s[i] < 48 || s[i] > 122:
			return false
		}
	}
	return true
}

// Close will attempt to close the connection to the database and stop any running services
// associated with the Linker struct.
func (l *Linker) Close() error {
	if l.db == nil {
		return nil
	}
	if err := l.db.Close(); err != nil {
		return &errval{s: "unable to close database", e: err}
	}
	if l.db = nil; l.get == nil {
		return nil
	}
	if err := l.get.Close(); err != nil {
		return &errval{s: "unable to close get statement", e: err}
	}
	l.get = nil
	select {
	case <-l.ctx.Done():
	default:
	}
	l.cancel()
	var (
		x, f = context.WithTimeout(context.Background(), defaultTimeout)
		err  = l.Server.Shutdown(x)
	)
	if f(); err != nil {
		return &errval{s: "unable to shutdown server", e: err}
	}
	l.ctx = nil
	return l.Server.Close()
}

// Listen will start the listing session for Linker to redirect HTTP requests. This function will block until the
// Close function is called or a SIGINT is received. This function will return an error if there is an issue
// during the listener creation.
func (l *Linker) Listen() error {
	if l.get != nil {
		return nil
	}
	var err error
	l.ctx, l.cancel = context.WithCancel(context.Background())
	if l.get, err = l.db.PrepareContext(l.ctx, sqlGet); err != nil {
		return &errval{s: "unable to prepare get statement", e: err}
	}
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go l.listen(&err)
	select {
	case <-s:
	case <-l.ctx.Done():
	}
	close(s)
	if l.cancel(); err != nil {
		l.Close()
		return err
	}
	return l.Close()
}
func (e errval) Error() string {
	if e.e == nil {
		return e.s
	}
	return e.s + ": " + e.e.Error()
}
func (e errval) Unwrap() error {
	return e.e
}
func expand(s string, l int) string {
	if len(s) >= l {
		return s
	}
	b := make([]byte, l)
	copy(b, s)
	for i := len(s); i < l; i++ {
		b[i] = 32
	}
	return string(b)
}
func (l *Linker) listen(err *error) {
	l.Server.Handler.(*http.ServeMux).HandleFunc("/", l.serve)
	if len(l.cert) == 0 || len(l.key) == 0 {
		*err = l.Server.ListenAndServe()
		l.cancel()
		return
	}
	l.Server.TLSConfig = &tls.Config{
		NextProtos: []string{"h2", "http/1.1"},
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		CurvePreferences:         []tls.CurveID{tls.CurveP256, tls.X25519},
		PreferServerCipherSuites: true,
	}
	*err = l.Server.ListenAndServeTLS(l.cert, l.key)
	l.cancel()
}

// New creates a new Linker instance and attempts to gather the initial configuration from a JSON formatted file.
// The path to this file can be passed in the string argument or read from the "LINKER_CONFIG" environment variable.
// This function will return an error if the load could not happen on the configuration file is invalid.
func New(s string) (*Linker, error) {
	l := &Linker{Server: http.Server{Handler: new(http.ServeMux)}}
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
	b, err := ioutil.ReadFile(s)
	if err != nil {
		return &errval{s: `unable to read file "` + s + `"`, e: err}
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return &errval{s: `unable to parse file "` + s + `"`, e: err}
	}
	if len(c.Database.Username) == 0 || len(c.Database.Server) == 0 || len(c.Database.Name) == 0 {
		return &errval{s: `file "` + s + `" does not contain a valid database configuration`}
	}
	if l.db, err = sql.Open("mysql", c.Database.Username+":"+c.Database.Password+"@"+c.Database.Server+"/"+c.Database.Name); err != nil {
		return &errval{s: `unable to connect to database "` + c.Database.Name + `" on "` + c.Database.Server + `"`, e: err}
	}
	if err = l.db.Ping(); err != nil {
		return &errval{s: `unable to connect to database "` + c.Database.Name + `" on "` + c.Database.Server + `"`, e: err}
	}
	n, err := l.db.Prepare(sqlPrepare)
	if err != nil {
		l.db.Close()
		return &errval{s: `unable to prepare the initial database table in "` + c.Database.Name + `" on "` + c.Database.Server + `"`, e: err}
	}
	_, err = n.Exec()
	if n.Close(); err != nil {
		l.db.Close()
		return &errval{s: `unable to create the initial database table in "` + c.Database.Name + `" on "` + c.Database.Server + `"`, e: err}
	}
	if len(c.Default) > 0 {
		u, err := url.Parse(c.Default)
		if err != nil {
			l.db.Close()
			return &errval{s: `unable to parse default URL "` + c.Default + `"`, e: err}
		}
		if !u.IsAbs() {
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
	return nil
}

// Add will attempt to add a redirect with the name of the first string to the URL provided in the second
// string argument. This function will return an error if the add fails.
func (l *Linker) Add(n, u string) error {
	if l.db == nil {
		return errNotConfigured
	}
	if !validName(n) {
		return &errval{s: `name "` + n + `" contains invalid characters`}
	}
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil {
		return &errval{s: `invalid URL "` + u + `"`, e: err}
	}
	if !p.IsAbs() {
		p.Scheme = "https"
	}
	q, err := l.db.Prepare(sqlAdd)
	if err != nil {
		return &errval{s: "unable to prepare add statement", e: err}
	}
	_, err = q.Exec(n, p.String())
	if q.Close(); err != nil {
		return &errval{s: "unable to execute add statement", e: err}
	}
	return nil
}

// Delete will attempt to remove the redirect name and URL using the mapping name. This function will return
// an error if the deletion fails. This function will pass even if the URL does not exist.
func (l *Linker) Delete(n string) error {
	if l.db == nil {
		return errNotConfigured
	}
	if !validName(n) {
		return &errval{s: `name "` + n + `" contains invalid characters`}
	}
	q, err := l.db.Prepare(sqlDelete)
	if err != nil {
		return &errval{s: "unable to prepare delete statement", e: err}
	}
	_, err = q.Exec(n)
	if q.Close(); err != nil {
		return &errval{s: "unable to execute delete statement", e: err}
	}
	return nil
}
func (l *Linker) context(_ net.Listener) context.Context {
	return l.ctx
}
func (l *Linker) serve(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if err := recover(); err != nil {
			os.Stderr.WriteString("HTTP function recovered from a panic: ")
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	if r.Body.Close(); len(r.RequestURI) <= 1 {
		http.Redirect(w, r, l.url, http.StatusTemporaryRedirect)
		return
	}
	var (
		s = html.EscapeString(r.RequestURI)
		p = regCheckURL.FindStringIndex(s)
	)
	if p == nil || p[0] != 0 || p[1] <= 1 {
		http.Redirect(w, r, l.url, http.StatusTemporaryRedirect)
		return
	}
	n, x := "", s[1:p[1]]
	if err := l.get.QueryRowContext(l.ctx, x).Scan(&n); err != nil {
		if err == sql.ErrNoRows {
			http.Redirect(w, r, l.url, http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`Could not fetch requested URL "` + x + `"`))
		os.Stderr.WriteString("HTTP function received an error: " + err.Error() + "!\n")
		return
	}
	if len(n) == 0 {
		http.Redirect(w, r, l.url, http.StatusTemporaryRedirect)
		return
	}
	if p[1] < len(s) {
		n = n + s[p[1]:]
	}
	http.Redirect(w, r, n, http.StatusTemporaryRedirect)
}
