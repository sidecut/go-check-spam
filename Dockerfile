FROM golang:1.27-alpine AS build

RUN apk add --no-cache build-base

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/gocheckspam .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates libgcc libstdc++

COPY --from=build /out/gocheckspam /usr/local/bin/gocheckspam

WORKDIR /data
VOLUME ["/data"]

ENTRYPOINT ["/usr/local/bin/gocheckspam"]