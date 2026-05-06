# Stage 1: Build the Go WhatsApp Bot
FROM golang:1.22-alpine AS bot-builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app/bot
COPY bot/go.mod bot/go.sum ./
RUN go mod download
COPY bot/ .
# Build the bot binary
RUN go build -o banabot main.go

# Stage 2: Final Production Image (PHP 8.4 + Go + Nginx)
FROM php:8.4-fpm-alpine

# Install system dependencies
RUN apk add --no-cache \
    nginx \
    supervisor \
    postgresql-dev \
    libpq \
    sqlite-libs \
    icu-dev \
    libzip-dev \
    zip \
    unzip

# Install PHP extensions for Laravel and Supabase
RUN docker-php-ext-install pdo pdo_pgsql pgsql intl zip

# Set working directory
WORKDIR /app

# Copy Laravel files
COPY laravel/ /app/laravel
RUN chown -R www-data:www-data /app/laravel/storage /app/laravel/bootstrap/cache

# Copy the Go Bot binary from Stage 1
COPY --from=bot-builder /app/bot/banabot /app/bot/banabot
# Create a folder for the WhatsApp session database
RUN mkdir -p /app/bot/data && chmod -R 777 /app/bot/data

# Copy Configuration Files
COPY nginx.conf /etc/nginx/http.d/default.conf
COPY supervisord.conf /etc/supervisord.conf

# Expose the port Hugging Face expects (7860)
EXPOSE 7860

# Start Supervisord to manage both processes
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisord.conf"]
