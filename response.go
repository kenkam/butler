package butler

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"log"
	"net/http"
	"strconv"
)

type Response struct {
	HttpVersion string
	StatusCode  int
	Headers     map[string]string
	Content     []byte
}

func Ok(content []byte) *Response {
	return &Response{"HTTP/1.1", http.StatusOK, make(map[string]string), content}
}

func NotFound() *Response {
	return &Response{"HTTP/1.1", http.StatusNotFound, make(map[string]string), nil}
}

func (r Response) Bytes(compressGzip bool) []byte {
	var buffer bytes.Buffer
	rLength := len(r.Content)

	if compressGzip {
		gzipWriter, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
		if err != nil {
			log.Fatal(err)
		}

		_, err = gzipWriter.Write(r.Content)
		if err != nil {
			log.Fatal(err)
		}

		gzipWriter.Close()
		rLength = buffer.Len()

		r.Headers[HeaderContentEncoding] = "gzip"
	}

	statusCode := fmt.Sprintf("%s %d %s\n", r.HttpVersion, r.StatusCode, http.StatusText(r.StatusCode))

	r.Headers[HeaderContentLength] = strconv.Itoa(rLength)

	b := []byte{}
	b = append(b, []byte(statusCode)...)
	b = append(b, r.headerBytes()...)
	b = append(b, []byte("\n")...)

	if compressGzip {
		b = append(b, buffer.Bytes()...)
	} else {
		b = append(b, r.Content...)
	}

	return b
}

func (r Response) headerBytes() []byte {
	b := []byte{}
	for k, v := range r.Headers {
		b = append(b, []byte(fmt.Sprintf("%s: %s\n", k, v))...)
	}
	return b
}
