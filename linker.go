// linker.go
// URL Shortener with MySQL database.
//
// Copyright (C) 2020 - 2023 iDigitalFlame
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
	"errors"
	"html"
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

// Defaults is a string representation of the default configuration for Linker.
// This can be used in a JSON file to configure a Linker instance.
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
	sqlPrepare = `CREATE TABLE IF NOT EXISTS Links (LinkID BIGINT(64) UNSIGNED NOT NULL PRIMARY KEY AUTO_INCREMENT,
		LinkName VARCHAR(64) NOT NULL UNIQUE, LinkURL VARCHAR(1024) NOT NULL)`

	defaultURL     = `https://duckduckgo.com`
	defaultFile    = `/etc/linker.conf`
	defaultTimeout = 5 * time.Second

	goGetQuery = "go-get=1"
)

var regCheckURL = regexp.MustCompile(`(^\/[a-zA-Z0-9]+)`)

// Linker is a struct that contains the web service and SQL queries that support
// the Linker URL shortener.
type Linker struct {
	http.Server

	ctx            context.Context
	db             *sql.DB
	get            *sql.Stmt
	cancel         context.CancelFunc
	url, key, cert string
}
type config struct {
	Database database `json:"db"`
	Key      string   `json:"key"`
	Cert     string   `json:"cert"`
	Listen   string   `json:"listen"`
	Default  string   `json:"default"`
	Timeout  uint8    `json:"timeout"`
}
type database struct {
	Name     string `json:"name"`
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// List will gather and print all the current link dataset.
//
// This function returns an error if there is an error reading from the database.
func (l *Linker) List() error {
	if l.db == nil {
		return errors.New("database is not loaded or configured")
	}
	q, err := l.db.Prepare(sqlList)
	if err != nil {
		return errors.New("prepare error: " + err.Error())
	}
	r, err := q.Query()
	if err != nil {
		q.Close()
		return errors.New("execute error: " + err.Error())
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
		return errors.New("parse error: " + err.Error())
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

// Close will attempt to close the connection to the database and stop any
// running services associated with the Linker struct.
func (l *Linker) Close() error {
	if l.db == nil {
		return nil
	}
	if err := l.db.Close(); err != nil {
		return errors.New("close error: " + err.Error())
	}
	if l.db = nil; l.get == nil {
		return nil
	}
	if err := l.get.Close(); err != nil {
		return errors.New("close get error: " + err.Error())
	}
	l.get = nil
	select {
	case <-l.ctx.Done():
	default:
	}
	l.cancel()
	var (
		x, f = context.WithTimeout(context.Background(), defaultTimeout)
		err  = l.Shutdown(x)
	)
	if f(); err != nil {
		return errors.New("shutdown error: " + err.Error())
	}
	l.ctx = nil
	return l.Server.Close()
}

// Listen will start the listing session for Linker to redirect HTTP requests.
// This function will block until the Close function is called or a SIGINT is
// received.
//
// This function will return an error if there is an issue during the listener
// creation.
func (l *Linker) Listen() error {
	if l.get != nil {
		return nil
	}
	var err error
	l.ctx, l.cancel = context.WithCancel(context.Background())
	if l.get, err = l.db.PrepareContext(l.ctx, sqlGet); err != nil {
		return errors.New("prepare get error: " + err.Error())
	}
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go l.listen(&err)
	select {
	case <-s:
	case <-l.ctx.Done():
	}
	signal.Stop(s)
	close(s)
	if l.cancel(); err != nil {
		l.Close()
		return err
	}
	return l.Close()
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
		if len(l.Addr) > 5 && (l.Addr[0] == 'u' || l.Addr[0] == 'U') && (l.Addr[3] == 'x' || l.Addr[3] == 'X') {
			n, e := net.Listen("unix", l.Addr[5:])
			if e != nil {
				*err = e
				l.cancel()
				return
			}
			if e = l.Serve(n); e != nil && e != http.ErrServerClosed {
				*err = e
			}
			l.cancel()
			return
		}
		if e := l.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			*err = e
		}
		l.cancel()
		return
	}
	l.TLSConfig = &tls.Config{
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
		CurvePreferences: []tls.CurveID{tls.CurveP256, tls.X25519},
	}
	if len(l.Addr) > 5 && (l.Addr[0] == 'u' || l.Addr[0] == 'U') && (l.Addr[3] == 'x' || l.Addr[3] == 'X') {
		n, e := net.Listen("unix", l.Addr[5:])
		if e != nil {
			*err = e
			l.cancel()
			return
		}
		if e = l.Serve(tls.NewListener(n, l.TLSConfig)); e != nil && e != http.ErrServerClosed {
			*err = e
		}
		l.cancel()
		return
	}
	if e := l.ListenAndServeTLS(l.cert, l.key); e != nil && e != http.ErrServerClosed {
		*err = e
	}
	l.cancel()
}

// New creates a new Linker instance and attempts to gather the initial
// configuration from a JSON formatted file. The path to this file can be
// passed in the string argument or read from the "LINKER_CONFIG" environment
// variable.
//
// This function will return an error if the load could not happen on the
// configuration file is invalid.
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
	b, err := os.ReadFile(s)
	if err != nil {
		return errors.New(`read "` + s + `": ` + err.Error())
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return errors.New(`parse "` + s + `": ` + err.Error())
	}
	if len(c.Database.Username) == 0 || len(c.Database.Server) == 0 || len(c.Database.Name) == 0 {
		return errors.New(`file "` + s + `" does not contain a valid configuration`)
	}
	if l.db, err = sql.Open("mysql", c.Database.Username+":"+c.Database.Password+"@"+c.Database.Server+"/"+c.Database.Name); err != nil {
		return errors.New(`connect "` + c.Database.Name + `" on "` + c.Database.Server + `" error: ` + err.Error())
	}
	if err = l.db.Ping(); err != nil {
		return errors.New(`connect "` + c.Database.Name + `" on "` + c.Database.Server + `" error: ` + err.Error())
	}
	n, err := l.db.Prepare(sqlPrepare)
	if err != nil {
		l.db.Close()
		return errors.New(`prepare table "` + c.Database.Name + `" on "` + c.Database.Server + `" error: ` + err.Error())
	}
	_, err = n.Exec()
	if n.Close(); err != nil {
		l.db.Close()
		return errors.New(`create table "` + c.Database.Name + `" on "` + c.Database.Server + `" error: ` + err.Error())
	}
	if len(c.Default) > 0 {
		u, err := url.Parse(c.Default)
		if err != nil {
			l.db.Close()
			return errors.New(`parse default URL "` + c.Default + `": ` + err.Error())
		}
		if !u.IsAbs() {
			u.Scheme = "https"
		}
		l.url = u.String()
	}
	if len(l.url) == 0 {
		l.url = defaultURL
	}
	l.Addr, l.key, l.cert = c.Listen, c.Key, c.Cert
	l.BaseContext, l.ReadTimeout = l.context, time.Second*time.Duration(c.Timeout)
	l.IdleTimeout, l.WriteTimeout, l.ReadHeaderTimeout = l.ReadTimeout, l.ReadTimeout, l.ReadTimeout
	return nil
}

// Add will attempt to add a redirect with the name of the first string to the
// URL provided in the second string argument.
//
// This function will return an error if the add fails.
func (l *Linker) Add(n, u string) error {
	if l.db == nil {
		return errors.New("database is not loaded or configured")
	}
	if !validName(n) {
		return errors.New(`name "` + n + `" contains invalid characters`)
	}
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil {
		return errors.New(`parse URL "` + u + `": ` + err.Error())
	}
	if !p.IsAbs() {
		p.Scheme = "https"
	}
	q, err := l.db.Prepare(sqlAdd)
	if err != nil {
		return errors.New("prepare add error: " + err.Error())
	}
	_, err = q.Exec(n, p.String())
	if q.Close(); err != nil {
		return errors.New("add error: " + err.Error())
	}
	return nil
}

// Delete will attempt to remove the redirect name and URL using the mapping name.
//
// This function will return an error if the deletion fails. This function will
// pass even if the URL does not exist.
func (l *Linker) Delete(n string) error {
	if l.db == nil {
		return errors.New("database is not loaded or configured")
	}
	if !validName(n) {
		return errors.New(`name "` + n + `" contains invalid characters`)
	}
	q, err := l.db.Prepare(sqlDelete)
	if err != nil {
		return errors.New("prepare delete error: " + err.Error())
	}
	_, err = q.Exec(n)
	if q.Close(); err != nil {
		return errors.New("delete error: " + err.Error())
	}
	return nil
}
func (l *Linker) context(_ net.Listener) context.Context {
	return l.ctx
}
func (l *Linker) serve(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recover() != nil {
			os.Stderr.WriteString("HTTP function recovered from a panic!")
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
		os.Stderr.WriteString("HTTP function error: " + err.Error() + "!\n")
		return
	}
	if len(n) == 0 {
		http.Redirect(w, r, l.url, http.StatusTemporaryRedirect)
		return
	}
	if len(r.URL.RawQuery) == len(goGetQuery) && r.URL.RawQuery == goGetQuery && r.Method == http.MethodGet {
		redirectGo(w, r, s[0:p[1]], n)
		return
	}
	if p[1] < len(s) {
		n = n + s[p[1]:]
	}
	http.Redirect(w, r, n, http.StatusTemporaryRedirect)
}
func redirectGo(w http.ResponseWriter, r *http.Request, x, n string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.Redirect(w, r, "https://pkg.go.dev/"+r.Host+x, http.StatusOK)
	w.Write(
		[]byte(
			`<!DOCTYPE html><html><head><meta name="go-import" content="` + r.Host + x +
				` git ` + n + `"><meta name="go-source" content="` + r.Host + x + ` ` + n +
				` ` + n + `/tree/master{/dir} ` + n + `/tree/master{/dir}/{file}#L{line}">` +
				`<meta http-equiv="refresh" content="0; url=` + n + `"></head><body>` +
				`No, no, no, go <a href="https://pkg.go.dev/` + r.Host + x + `">here</a>.</body></html>`,
		),
	)
}
