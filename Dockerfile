FROM golang:1.23 AS builder

COPY . /go/src/app
WORKDIR /go/src/app

ENV GO111MODULE=on

RUN CGO_ENABLED=0 GOOS=linux make build

RUN git log -1 --oneline > version.txt

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /go/src/app/reporting-service .
COPY --from=builder /go/src/app/version.txt .

EXPOSE 8080

LABEL org.opencontainers.image.source=https://github.com/SENERGY-Platform/reporting-service

ENTRYPOINT ["./reporting-service"]