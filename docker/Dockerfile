FROM golang:1.24-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o main cmd/api/main.go

FROM alpine:3.21.3 AS executor

WORKDIR /app

COPY --from=build /app/main /app/main
# TODO: EXPOSE PORTS WHEN WE SET UP THE WEB SERVER
CMD ["./main"]
