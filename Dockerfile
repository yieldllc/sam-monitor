FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/sam-monitor .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sam-monitor /sam-monitor
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/sam-monitor"]
