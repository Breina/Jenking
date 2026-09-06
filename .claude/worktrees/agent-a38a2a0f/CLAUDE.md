# Jenking

A terminal user interface for Jenkins, inspired by k9s and Argonaut.

## Tech Stack
- Language: Go
- TUI Framework: Bubbletea
- Target: Linux/macOS

## Build & Run
```bash
go run ./cmd/jenkins-tui
go build -o jenkins-tui ./cmd/jenkins-tui
go test ./...
```

## Project Conventions
- Vertical slice development: each feature is a thin, working slice
- Every slice must compile and run before moving to the next
- Test as we go, not after
- Code must be written to be maintainable; follow clean code principles
- Tell the user it's a good moment to split off a subtask to an agent

## Core Features
1. View currently running builds
2. Follow build logs (live streaming)
3. Start a build (with parameter support)
4. Cancel a build
5. See at which phase/stage a build failed

## Target Users
- Sysadmins managing Jenkins
- Developers using Jenkins daily
