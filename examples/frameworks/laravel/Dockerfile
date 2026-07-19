ARG LARAVEL_SKELETON_VERSION=13.8.0
ARG COMPOSER_IMAGE=composer:2.10.2@sha256:5946476338742b200bb9ff88f8be56275ddae4b3949c72305cb0dbf10cfcb760
ARG NODE_IMAGE=node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd
ARG FRANKENPHP_IMAGE=dunglas/frankenphp:1.12.3-php8.5-alpine@sha256:19eda5f22186afeda3aaa70f103a7019bbcff57980da8069f7861c1034aa81ae

FROM ${COMPOSER_IMAGE} AS source
ARG LARAVEL_SKELETON_VERSION
WORKDIR /app
RUN composer create-project --no-interaction --prefer-dist --no-install --no-scripts laravel/laravel:${LARAVEL_SKELETON_VERSION} .

FROM ${FRANKENPHP_IMAGE} AS base
RUN install-php-extensions pdo_sqlite \
    && mv "$PHP_INI_DIR/php.ini-production" "$PHP_INI_DIR/php.ini"

FROM base AS dependencies
COPY --from=source /usr/bin/composer /usr/bin/composer
COPY --from=source /app /app
WORKDIR /app
RUN composer install --no-dev --no-interaction --no-ansi --prefer-dist --optimize-autoloader

FROM ${NODE_IMAGE} AS assets
WORKDIR /app
COPY --from=source /app/package.json /app/.npmrc ./
COPY --from=source /app/resources ./resources
COPY --from=source /app/vite.config.js ./
RUN npm install --no-audit --no-fund \
    && npm run build

FROM dependencies AS runtime-output
COPY --from=assets /app/public/build ./public/build
RUN rm -rf \
        .editorconfig \
        .env.example \
        .gitattributes \
        .gitignore \
        .npmrc \
        README.md \
        composer.json \
        composer.lock \
        database/factories \
        database/seeders \
        package.json \
        package-lock.json \
        phpunit.xml \
        resources/css \
        resources/js \
        storage/framework/testing \
        tests \
        vite.config.js \
    && find . -name .gitignore -delete \
    && mkdir -p storage/framework/cache/data storage/framework/sessions storage/framework/views storage/logs \
    && touch storage/database.sqlite

FROM base AS runtime
ENV SERVER_NAME=:8080 \
    SERVER_ROOT=public/
WORKDIR /app
COPY --from=runtime-output --chown=www-data:www-data /app ./
RUN mkdir -p /config/caddy /data/caddy \
    && chown -R www-data:www-data /config /data bootstrap/cache storage
USER www-data
EXPOSE 8080
