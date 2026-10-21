# Подхватываем .env (если есть) и экспортируем переменные в команды
-include .env
export

GOOSE          ?= goose
MIGRATIONS_DIR := migrations

.DEFAULT_GOAL := help
.PHONY: help up down reset-db psql migrate-up migrate-down migrate-status migrate-test \
        run build test test-race test-integration vet fmt

help: ## Показать список команд
ifeq ($(OS),Windows_NT)
	@powershell -NoProfile -Command "[Console]::OutputEncoding = [Text.Encoding]::UTF8; Select-String -Path '$(firstword $(MAKEFILE_LIST))' -Encoding UTF8 -Pattern '^([a-zA-Z_-]+):.*?## (.*)$$' | ForEach-Object { '  {0,-18} {1}' -f $$_.Matches[0].Groups[1].Value, $$_.Matches[0].Groups[2].Value }"
else
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'
endif

up: ## Поднять Postgres и дождаться готовности
	docker compose up -d --wait

down: ## Остановить контейнеры (данные сохраняются)
	docker compose down

reset-db: ## Удалить контейнеры вместе с данными
	docker compose down -v

psql: ## psql к основной базе
	docker compose exec db psql -U upwatch -d upwatch

migrate-up: ## Накатить миграции на основную базу
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(UPWATCH_DATABASE_URL)" up

migrate-down: ## Откатить последнюю миграцию
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(UPWATCH_DATABASE_URL)" down

migrate-status: ## Статус миграций
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(UPWATCH_DATABASE_URL)" status

migrate-test: ## Накатить миграции на тестовую базу
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(UPWATCH_TEST_DATABASE_URL)" up

run: ## Запустить сервис
	go run ./cmd/upwatch

build: ## Собрать бинарник в bin/
	go build -o bin/upwatch ./cmd/upwatch

test: ## Юнит-тесты (без БД)
	go test ./...

test-race: ## Тесты с race-детектором (на Windows нужен gcc)
	go test -race ./...

test-integration: ## Интеграционные тесты репозитория
	go test -tags integration -count=1 -v ./internal/storage/...

vet: ## go vet, включая файлы с тегом integration
	go vet ./...
	go vet -tags integration ./...

fmt: ## Отформатировать код
	gofmt -s -w .