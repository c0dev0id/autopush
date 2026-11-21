# Architecture

## High-level architecture

```mermaid
flowchart LR
    dev["Developer: git commit"] --> repo[("Local repository")]
    repo -- "COMMIT_EDITMSG changes" --> watcher["kqueue watcher"]
    watcher --> loop["Daemon loop (one per repository)"]
    tick["30 s ticker"] --> loop
    ptick["-p N pull ticker"] --> loop
    loop -- "pull --rebase / push" --> git["git CLI"]
    git <--> remote[("Git remote")]
    loop -- "-c" --> hook["sh -c command"]
    loop -- "start / cancel" --> ci["Workflow watch"]
    ci -- "REST polling while runs are in flight" --> gh["GitHub Actions API"]
    loop --> notify["Notifier"]
    ci --> notify
    notify --> out["stdout"]
    notify --> title["Terminal / tmux title"]
```

Oneshot mode (`-1`) runs the same sync step once per repository, waits for the
workflow watch in the foreground and exits with its result.

## Block diagram

```mermaid
flowchart TB
    subgraph cli["CLI — main.go"]
        flags["Flag parsing and argument rewriting"]
        resolve["Repository resolution: paths, -a, -r"]
        modes["Daemon loop / oneshot"]
    end
    subgraph sync["Sync step — main.go"]
        synchead["syncHead: pull, push"]
        target["workflowTarget: where CI can be watched"]
        hook["runOnChange: -c command"]
    end
    subgraph adapters["System adapters"]
        gitgo["git.go — git subprocesses"]
        githubgo["github.go — Actions REST client"]
        watchgo["watch.go — kqueue"]
        notifygo["notify.go — output and window title"]
    end
    flags --> resolve --> modes
    modes --> synchead
    modes --> target
    modes --> hook
    modes --> watchgo
    modes --> githubgo
    synchead --> gitgo
    target --> gitgo
    synchead --> notifygo
    githubgo --> notifygo
```

There is no database.
