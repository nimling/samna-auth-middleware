package swriter

import (
	"bytes"
	"net/http"
)

type CustomResponseWriter struct {
	http.ResponseWriter
	header      http.Header
	StatusCode  int
	body        bytes.Buffer
	wroteHeader bool // Track if headers were written
}

func NewCustomResponseWriter(w http.ResponseWriter) *CustomResponseWriter {
	return &CustomResponseWriter{
		ResponseWriter: w,
		header:         make(http.Header),
		StatusCode:     http.StatusOK, // Default status code
	}
}

func (c *CustomResponseWriter) Header() http.Header {
	return c.header
}

func (c *CustomResponseWriter) Write(b []byte) (int, error) {
	// If headers haven't been written yet, write them
	if !c.wroteHeader {
		c.WriteHeader(c.StatusCode)
	}

	// Only write to our buffer
	return c.body.Write(b)
}

func (c *CustomResponseWriter) WriteHeader(statusCode int) {
	if c.wroteHeader {
		return // Prevent multiple header writes
	}

	c.StatusCode = statusCode
	c.wroteHeader = true

	// Copy our custom headers to the underlying ResponseWriter
	for k, v := range c.header {
		c.ResponseWriter.Header()[k] = v
	}
}
