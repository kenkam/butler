package butler

import (
	"fmt"
	"net/http"
)

type Response struct {
	HttpVersion string
	StatusCode  int
	Content     []byte
}

func Ok(content []byte) *Response {
	return &Response{"HTTP/1.1", http.StatusOK, content}
}

func NotFound() *Response {
	return &Response{"HTTP/1.1", http.StatusNotFound, nil}
}

func (r Response) ToBytes() []byte {
	status := fmt.Sprintf("%s %d %s\n", r.HttpVersion, r.StatusCode, http.StatusText(r.StatusCode))
	contentLength := fmt.Sprintf("Content-Length: %d\n", len(r.Content))
	bytes := make([]byte, 0)
	bytes = append(bytes, []byte(status)...)
	bytes = append(bytes, []byte(contentLength)...)
	bytes = append(bytes, []byte("\n")...)
	bytes = append(bytes, r.Content...)
	return bytes
}
