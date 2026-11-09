FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server .

FROM alpine:3.20
RUN adduser -D -H sampleuser
USER sampleuser
COPY --from=build /out/server /server
ENV PORT=8080
EXPOSE 8080
CMD ["/server"]
