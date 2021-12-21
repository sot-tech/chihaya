FROM golang:alpine AS build-env
LABEL maintainer "Jimmy Zelinskie <jimmyzelinskie+git@gmail.com>"

# Install OS-level dependencies.
RUN apk add --no-cache curl git

# Copy our source code into the container.
WORKDIR /go/src/github.com/sot-tech/mochi
COPY . /go/src/github.com/sot-tech/mochi

# Install our golang dependencies and compile our binary.
RUN CGO_ENABLED=0 go install ./cmd/mochi

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=build-env /go/bin/mochi /mochi

RUN adduser -D mochi

# Expose a docker interface to our binary.
EXPOSE 6880 6969

# Drop root privileges
USER mochi

ENTRYPOINT ["/mochi"]
