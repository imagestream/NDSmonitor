# Joshua Snyder 11-12-2020

FROM golang:1.15.2-alpine3.12 as builder
RUN apk add --no-cache --virtual .build-deps gcc musl-dev openssl git openssh

RUN go get github.com/influxdata/influxdb1-client/v2
RUN go get gopkg.in/yaml.v2
RUN go get golang.org/x/crypto/ssh

RUN git clone https://github.com/imagestream/NDSmonitor.git
WORKDIR /go/NDSmonitor
RUN go build

FROM alpine:latest
COPY --from=builder /go/NDSmonitor/NDSmonitor . 
CMD ./NDSmonitor
