FROM golang:1.27-bookworm AS build

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN gcc -c -fPIC docker/alpine-compat.c -o /tmp/alpine-compat.o
RUN CGO_ENABLED=1 GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_LDFLAGS=/tmp/alpine-compat.o go build -trimpath -ldflags='-s -w' -o /out/gocheckspam .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates gcompat libgcc libstdc++

COPY --from=build /out/gocheckspam /usr/local/bin/gocheckspam

WORKDIR /data
VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/gocheckspam"]