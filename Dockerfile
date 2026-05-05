FROM php:8.4-fpm-alpine

# 1. Install System Dependencies
RUN apk add --no-cache \
    nginx \
    supervisor \
    go \
    git \
    unzip \
    libzip-dev \
    postgresql-dev \
    sqlite-dev \
    oniguruma-dev \
    libxml2-dev \
    linux-headers

# 2. Install PHP Extensions for Laravel & Postgres
RUN docker-php-ext-install \
    pdo \
    pdo_pgsql \
    pdo_sqlite \
    bcmath \
    mbstring \
    xml \
    ctype \
    fileinfo

# 3. Get Composer
COPY --from=composer:latest /usr/bin/composer /usr/bin/composer

# 4. Set up Project Directory
WORKDIR /app
COPY . .

# 5. Build the Go Bot
WORKDIR /app/bot
# Ensure the session data folder exists for WhatsApp
RUN mkdir -p /app/bot/data
RUN go mod tidy && go build -o banabot main.go

# 6. Set up Laravel
WORKDIR /app/laravel
RUN composer install --no-dev --no-scripts --optimize-autoloader
RUN cp .env.example .env

# Permissions for Laravel (Required for Hugging Face)
RUN chown -R www-data:www-data /app/laravel/storage /app/laravel/bootstrap/cache
RUN chmod -R 775 /app/laravel/storage /app/laravel/bootstrap/cache

# 7. Web Server & Process Configuration
WORKDIR /app
COPY ./nginx.conf /etc/nginx/http.d/default.conf
COPY ./supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# 8. Final Port & Start
EXPOSE 7860
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
