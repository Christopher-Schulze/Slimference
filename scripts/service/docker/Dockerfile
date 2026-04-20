# Minimal container image for Slimference. Multi-stage build produces a
# statically linked binary on a distroless base so the image stays tiny
# and there is no shell to exploit at runtime.

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=dev
RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/slimference \
      ./cmd/slimference

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/slimference /usr/local/bin/slimference

# Default: headless proxy bound to the container interface.
EXPOSE 8990
ENTRYPOINT ["/usr/local/bin/slimference", "--no-tui", "--log-format=json"]

# To build:
#   docker build -f scripts/service/docker/Dockerfile \
#     --build-arg VERSION=2.1.0 --build-arg COMMIT=$(git rev-parse --short HEAD) \
#     -t slimference:2.1.0 .
#
# To run:
#   docker run --rm -p 8990:8990 \
#     -e MINIMAX_API_KEY=$MINIMAX_API_KEY \
#     -v ~/.slimference:/root/.slimference \
#     slimference:2.1.0
