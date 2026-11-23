# b64 as a container image: docker run --rm -i ghcr.io/base64hub-com/b64 < file
# The build stage runs on the build host and cross-compiles, so no emulation is needed.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" -o /b64 ./cmd/b64

FROM scratch
COPY --from=build /b64 /b64
COPY LICENSE /LICENSE
USER 65534:65534
ENTRYPOINT ["/b64"]
