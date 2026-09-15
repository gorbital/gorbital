# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X apistock.dev/buildinfo.version=${VERSION}" \
    -o /out/ ./cmd/api ./cmd/migrate

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
COPY --from=build /out/migrate /migrate
ENV APP_ENV=production \
    APP_ADDR=0.0.0.0:8080
EXPOSE 8080
USER nonroot:nonroot
# Run migrations before each release: docker run --entrypoint /migrate <image>
ENTRYPOINT ["/api"]
