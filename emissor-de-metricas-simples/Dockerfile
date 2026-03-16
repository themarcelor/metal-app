FROM golang:1.21 AS build

WORKDIR /app/src/
COPY go.mod .
COPY go.sum .
COPY . .
RUN CGO_ENABLED=0 go build -o /main .

FROM alpine:3.14 AS run
WORKDIR /app
COPY --from=build /main .

ENTRYPOINT ["/app/main"]
