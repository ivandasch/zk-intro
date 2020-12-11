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

### Локальный браузер зукипера
Перейти по ссылке -- http://localhost:8000

### Lock
* Запуск watcher 
```
make watcher
```
* Запуск writer без правильной блокировки
```
make writer
```
* Запуск writer с блокировкой
```
make writer type=lock
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