FROM golang:1.24.2-bookworm AS build
 
WORKDIR /app 
COPY go.mod .
RUN go mod download
COPY . ./ 
RUN go test
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=$(git describe --abbrev=0 --tags)" -o butler cli/main.go

FROM alpine:3.21.3
LABEL org.opencontainers.image.source=https://github.com/kenkam/butler

WORKDIR /app
COPY --from=build /app/butler .

CMD [ "./butler" ]
