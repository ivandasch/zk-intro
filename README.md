# Примеры и паттерны использования Apache Zookeeper
## Презентация
https://ivandasch.github.io/zk-intro

## Установка зависимостей
* Требуется установленный `docker` и `docker-compose`
* Требуется установленный `go` (>= 1.15)
* Требуется установленный `make`
* Установка зависимостей
```
make dependencies
```

## Запуск примеров
### Запуск тестового Zookeeper
```
docker-compose up
```

### Lock
* Запуск watcher 
```
make watcher-lock
```
* Запуск writer без правильной блокировки
```
make writer-lock type=fake
```
* Запуск writer с блокировкой
```
make writer-lock
```
### Запуск узла discovery
```
make disco
```

## Сборка слайдов презентации
```
cd slides
make slides
```