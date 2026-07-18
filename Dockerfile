# build
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/links-service ./cmd/api

# runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /bin/links-service /links-service
EXPOSE 8080
ENTRYPOINT ["/links-service"]
