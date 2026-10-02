# syntax=docker/dockerfile:1

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/parceiros ./cmd/parceiros

# ffmpeg é usado pelo worker para processar os vídeos enviados pelos usuários.
FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates ffmpeg \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 10001 --no-create-home parceiros
COPY --from=build /out/parceiros /usr/local/bin/parceiros
USER 10001
EXPOSE 8080
ENTRYPOINT ["parceiros"]
CMD ["api"]
