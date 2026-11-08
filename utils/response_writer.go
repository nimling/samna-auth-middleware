package utils

import (
	"bytes"
	"net/http"
)

type CustomResponseWriter struct {
	http.ResponseWriter
	header      http.Header
	StatusCode  int
	body        bytes.Buffer
	wroteHeader bool
}

func NewCustomResponseWriter(w http.ResponseWriter) *CustomResponseWriter {
	return &CustomResponseWriter{
		ResponseWriter: w,
		header:         make(http.Header),
		StatusCode:     http.StatusOK,
	}
}

func (c *CustomResponseWriter) Header() http.Header {
	return c.header
}

func (c *CustomResponseWriter) Write(b []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(c.StatusCode)
	}
	return c.body.Write(b)
}

func (c *CustomResponseWriter) WriteHeader(statusCode int) {
	if c.wroteHeader {
		return
	}

	c.StatusCode = statusCode
	c.wroteHeader = true

	for k, v := range c.header {
		c.ResponseWriter.Header()[k] = v
	}
}
