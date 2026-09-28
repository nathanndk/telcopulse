FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,id=telcopulse-go-mod,target=/go/pkg/mod,sharing=locked go mod download
COPY . .
ARG SERVICE=api-gateway
# Keep compiler intermediates out of disk-backed layers on constrained local hosts.
RUN --mount=type=cache,id=telcopulse-go-mod,target=/go/pkg/mod,sharing=locked --mount=type=tmpfs,target=/root/.cache/go-build --mount=type=tmpfs,target=/tmp if [ "$SERVICE" = "api-gateway" ]; then target=./api-gateway/cmd/api; else target=./$SERVICE/cmd; fi; CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /service "$target"
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -g 10001 app && adduser -D -u 10001 -G app app
COPY --from=build /service /usr/local/bin/service
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/service"]
