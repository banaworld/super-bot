# Stage 1: Build the Go WhatsApp Bot
FROM golang:1.24-alpine AS bot-builder

# Install git, gcc, and necessary build tools
RUN apk add --no-cache git gcc musl-dev sqlite-dev

WORKDIR /app/bot

# Copy only module files first for better caching
COPY bot/go.mod bot/go.sum ./

# Git is now available to download dependencies like whatsmeow
RUN go mod download

# Copy the rest of the bot source code
COPY bot/ .

# Build the bot binary
RUN go build -o banabot main.go

# Stage 2: Final Production Image (PHP 8.4 + Go + Nginx)
FROM php:8.4-fpm-alpine

# Install system dependencies for Laravel, Nginx, and Supervisor
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

# Install PHP extensions required for Laravel and Supabase (Seoul)
RUN docker-php-ext-install pdo pdo_pgsql pgsql intl zip

# Set working directory for the full project
WORKDIR /app

# Copy Laravel files into the container
COPY laravel/ /app/laravel

# Set permissions so Laravel can write to storage and cache
RUN chown -R www-data:www-data /app/laravel/storage /app/laravel/bootstrap/cache

# Copy the compiled Go Bot binary from the first stage
COPY --from=bot-builder /app/bot/banabot /app/bot/banabot

# Create a persistent folder for your WhatsApp session database
RUN mkdir -p /app/bot/data && chmod -R 777 /app/bot/data

# Copy Nginx and Supervisor configuration files
COPY nginx.conf /etc/nginx/http.d/default.conf
COPY supervisord.conf /etc/supervisord.conf

# Expose the standard Hugging Face Space port
EXPOSE 7860

# Launch Supervisor to manage both the Laravel server and Go Bot
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisord.conf"]
