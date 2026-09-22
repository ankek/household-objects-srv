package middleware

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

type recorder struct {
	http.ResponseWriter

	status int

	wrote bool

	bytes int64
}

func wrap(w http.ResponseWriter) *recorder {
	if rec, ok := w.(*recorder); ok {
		return rec
	}
	return &recorder{ResponseWriter: w}
}

func unwrap(w http.ResponseWriter) http.ResponseWriter {
	for {
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return w
		}
		w = u.Unwrap()
	}
}

func (rec *recorder) WriteHeader(status int) {
	if rec.wrote {
		return
	}
	rec.status, rec.wrote = status, true
	rec.ResponseWriter.WriteHeader(status)
}

func (rec *recorder) Write(b []byte) (int, error) {
	rec.observeHeader()
	n, err := rec.ResponseWriter.Write(b)
	rec.bytes += int64(n)
	return n, err
}

func (rec *recorder) observeHeader() {
	if !rec.wrote {
		rec.status, rec.wrote = http.StatusOK, true
	}
}

func (rec *recorder) Unwrap() http.ResponseWriter { return rec.ResponseWriter }

func (rec *recorder) Flush() {
	rec.observeHeader()
	_ = http.NewResponseController(rec.ResponseWriter).Flush()
}

func (rec *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(rec.ResponseWriter).Hijack()
}

func (rec *recorder) ReadFrom(src io.Reader) (int64, error) {
	rec.observeHeader()
	n, err := io.Copy(rec.ResponseWriter, src)
	rec.bytes += n
	return n, err
}
