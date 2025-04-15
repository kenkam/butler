# Butler

A http server written in go, for learning.

## TODO

* [x] GET requests
  * [x] Compression
* [x] HEAD requests
* [x] Keep alives
* [x] Testing
* [x] SSL / TLS
* [ ] Read from config
  * [x] Initial implementation
  * [ ] Refactor listen / listenTLS to have a unifed Listen method, validate config
* [x] Redirect HTTP -> HTTPS
* [ ] Proxy backends
  * [x] Impl
  * [ ] Tests
* [ ] Backend registration / healthchecks
* [ ] Go versioning
* [ ] CI/CD
* [ ] POST / PUT requests via a cgi-bin like interface
* [ ] Caches

HTTP/1.1 Spec: https://www.rfc-editor.org/rfc/rfc9110.html#name-example-message-exchange
