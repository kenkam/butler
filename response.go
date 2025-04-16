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
	Headers     map[string][]string
	Content     []byte
}

func Ok(content []byte) *Response {
	return StatusCode(http.StatusOK, content)
}

const hTemplate = `<HTML><HEAD><meta http-equiv="content-type" content="text/html;charset=utf-8">
<TITLE>%v</TITLE></HEAD><BODY>
<H1>%v</H1>
</BODY></HTML>
`

func MovedPermanently(location string) *Response {
	msg := fmt.Sprintf("%v %v", http.StatusMovedPermanently, "Moved")
	r := StatusCode(http.StatusMovedPermanently, fmt.Appendf(nil, hTemplate, msg, msg))

	r.Headers[HeaderLocation] = []string{location}
	return r
}

func BadGateway() *Response {
	msg := fmt.Sprintf("%v %v", http.StatusBadGateway, "Bad Gateway")
	return StatusCode(http.StatusBadGateway, fmt.Appendf(nil, hTemplate, msg, msg))
}

func NotFound() *Response {
	msg := fmt.Sprintf("%v %v", http.StatusNotFound, "Not Found")
	return StatusCode(http.StatusNotFound, fmt.Appendf(nil, hTemplate, msg, msg))
}

func StatusCode(statusCode int, content []byte) *Response {
	return &Response{"HTTP/1.1", statusCode, make(map[string][]string), content}
}

func (r Response) Bytes(compressGzip bool, headersOnly bool) []byte {
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

		r.Headers[HeaderContentEncoding] = []string{"gzip"}
	}

	statusCode := fmt.Sprintf("%s %d %s\n", r.HttpVersion, r.StatusCode, http.StatusText(r.StatusCode))

	if rLength > 0 {
		r.Headers[HeaderContentLength] = []string{strconv.Itoa(rLength)}
	}

	b := []byte{}
	b = append(b, []byte(statusCode)...)
	b = append(b, r.headerBytes()...)
	b = append(b, []byte("\n")...)

	if headersOnly {
		return b
	}

	if compressGzip {
		b = append(b, buffer.Bytes()...)
	} else {
		b = append(b, r.Content...)
	}

	return b
}

func (r Response) headerBytes() []byte {
	b := []byte{}
	for k, vs := range r.Headers {
		for _, v := range vs {
			b = fmt.Appendf(b, "%s: %s\n", k, v)
		}
	}
	return b
}
