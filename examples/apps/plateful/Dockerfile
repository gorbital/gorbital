# syntax=docker/dockerfile:1

# docs:start dockerfile

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X gorbital.dev/buildinfo.version=${VERSION}" \
    -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
ENV APP_ENV=production \
    APP_ADDR=0.0.0.0:8080
EXPOSE 8080
USER nonroot:nonroot
# Run migrations before each release: docker run <image> migrate
ENTRYPOINT ["/api"]

# docs:end dockerfile
