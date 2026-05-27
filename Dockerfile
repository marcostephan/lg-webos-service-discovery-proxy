# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -ldflags="-s -w" -trimpath -o /out/lgtv-sdp .

FROM scratch
COPY --from=build /out/lgtv-sdp /lgtv-sdp
EXPOSE 80 443
USER 65534:65534
ENTRYPOINT ["/lgtv-sdp"]
