# git

[English](README.md) · [Довідник операцій](DOC.UK.md) · [Історія змін](CHANGELOG.md)

Модуль запускає встановлений Git для роботи з локальними робочими копіями й
bare-репозиторіями. Окремий пакет `githttp` дозволяє клонування, отримання й
надсилання змін через smart HTTP. Обидва пакети використовують стандартну
бібліотеку Go.

## Встановлення

Потрібні Go 1.24+ і Git 2.43+. Для локальної версії додайте модуль у робочий
простір командою `go work use /шлях/до/git`. Після публікації тегу командою
встановлення буде `go get github.com/goloop/git@v0.1.0`.

## Читання робочої копії

```go
client, err := git.New(git.ClientOptions{})
if err != nil { return err }
repo, err := client.Open(ctx, checkout)
if err != nil { return err }
head, err := repo.Head(ctx)
if errors.Is(err, git.ErrUnbornHEAD) { return nil }
if err != nil { return err }
entry, err := repo.CommitInfo(ctx, string(head.ID))
if err != nil { return err }
fmt.Println(entry.Subject)
```

Для bare-репозиторію викличте
`client.Init(ctx, path, git.InitOptions{Bare: true, InitialBranch: "main"})`.
Для серверного перегляду використовуйте
`client.OpenManaged(ctx, "/srv/git", "team/tools.git")`. Сервіс повинен
володіти деревом каталогів і метаданими репозиторію.

## Потокове читання

```go
blob, err := repo.Blob(ctx, "main:README.md")
if err != nil { return err }
defer blob.Close()
_, err = io.Copy(dst, blob)
return err
```

Для малого файлу використовуйте `ReadFile` з лімітом байтів. Читання потоку
може повернути помилку процесу після видачі перших байтів. Причини скасування
та відомі стани перевіряйте через `errors.Is`, код завершення Git — через
`errors.As` з `*git.CommandError`.

## Власний сервер

[Робочий приклад](examples/browser/main.go) віддає дерево HEAD у JSON і
дозволяє клонування через HTTP. Задайте `REPO_ROOT`, `REPO_ID` і
`REPO_PASSWORD`, запустіть `go run ./examples/browser` та використайте
користувача `reader`. Сервер слухає loopback. Застосунок відповідає за
TLS, ідентифікацію, перевірку членства в команді та інтерфейс користувача.
Коли потрібні облікові дані, middleware перед githttp має повернути 401
із заголовком WWW-Authenticate. Відмова в дозволі на репозиторій повертає 403.

Звичайні методи відкриття, налаштування й синхронізації призначені для
довірених репозиторіїв і транспортних адрес. Git може запускати налаштовані
хуки й допоміжні програми. Серверний режим приймає обмежену конфігурацію.
Розділяйте файлові та мережеві права орендарів засобами ОС, обмежуйте CPU,
диск і пам’ять, оновлюйте Git. Модуль орієнтовано на Linux, macOS і Windows.
У Unix скасування завершує групу процесів, у Windows — безпосередній процес.
