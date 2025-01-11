FROM golang:latest
RUN apt-get update && apt-get install -y \
ffmpeg

ADD . /go/src/github.com/cj123/quotedb

WORKDIR /go/src/github.com/cj123/quotedb

RUN go get .
RUN go build .

EXPOSE 10443

ENTRYPOINT /go/src/github.com/cj123/quotedb/quotedb
