# Etap 1: Budowanie aplikacji
FROM golang:1.26-alpine AS builder

# Instalacja niezbędnych narzędzi do budowania
RUN apk add --no-cache git

WORKDIR /app

# Kopiowanie plików zależności i pobieranie ich
COPY go.mod go.sum ./
RUN go mod download

# Kopiowanie kodu źródłowego
COPY . .

# Kompilacja aplikacji
RUN CGO_ENABLED=0 GOOS=linux go build -o recipe_importer_ai main.go

# Etap 2: Finalny obraz
FROM alpine:latest

# Instalacja certyfikatów CA (wymagane do połączeń HTTPS z API)
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Kopiowanie skompilowanej binarki z poprzedniego etapu
COPY --from=builder /app/recipe_importer_ai .

# Eksponowanie portu (domyślnie 8080)
EXPOSE 8080

# Uruchomienie aplikacji
CMD ["./recipe_importer_ai"]
