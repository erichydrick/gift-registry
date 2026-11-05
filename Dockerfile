FROM golang:1.27.1-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o main main.go

FROM alpine:3.22.2 AS executor

WORKDIR /app

RUN apk add --no-cache libgcc gcompat

COPY --from=build /app/main /app/main
CMD ["./main"]
