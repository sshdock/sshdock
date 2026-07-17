FROM node:24.18.0-alpine3.24 AS assets

WORKDIR /app

COPY package.json package-lock.json .npmrc ./
RUN npm ci

COPY resources ./resources
COPY vite.config.js ./
RUN npm run build

FROM dunglas/frankenphp:1.12.3-php8.5-alpine AS base

RUN install-php-extensions pdo_sqlite \
    && mv "$PHP_INI_DIR/php.ini-production" "$PHP_INI_DIR/php.ini"

FROM base AS vendor

COPY --from=composer:2.10.2 /usr/bin/composer /usr/bin/composer
WORKDIR /app
COPY . .
RUN composer install --no-dev --no-interaction --no-ansi --prefer-dist --optimize-autoloader

FROM base AS runtime

ENV SERVER_NAME=:8080
ENV SERVER_ROOT=public/
WORKDIR /app

COPY --chown=www-data:www-data . .
COPY --from=vendor --chown=www-data:www-data /app/vendor ./vendor
COPY --from=assets --chown=www-data:www-data /app/public/build ./public/build

RUN mkdir -p /config/caddy /data/caddy storage/framework/cache/data storage/framework/sessions storage/framework/views storage/logs \
    && chown -R www-data:www-data /config /data bootstrap/cache storage

USER www-data
EXPOSE 8080
