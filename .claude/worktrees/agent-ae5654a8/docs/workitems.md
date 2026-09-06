Open bugs
1. Whem running a build that has no prior estimate, opening it either in Running Builds or Jobs, crashes the app;
  ```
Caught panic:

runtime error: slice bounds out of range [-3081520:]

Restoring terminal...

goroutine 1 [running]:
runtime/debug.Stack()
        /home/brecht/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.24.2.linux-amd64/src/runtime/debug/stack.go:26 +0x5e
runtime/debug.PrintStack()
        /home/brecht/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.24.2.linux-amd64/src/runtime/debug/stack.go:18 +0x13
github.com/charmbracelet/bubbletea.(*Program).recoverFromPanic(0xc0003b8000, {0x85c600, 0xc0002a6198})
        /home/brecht/go/pkg/mod/github.com/charmbracelet/bubbletea@v1.3.10/tea.go:847 +0xac
github.com/charmbracelet/bubbletea.(*Program).Run.func2()
        /home/brecht/go/pkg/mod/github.com/charmbracelet/bubbletea@v1.3.10/tea.go:638 +0xe8
panic({0x85c600?, 0xc0002a6198?})
        /home/brecht/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.24.2.linux-amd64/src/runtime/panic.go:792 +0x132
github.com/brecht/jenkins-tui/internal/tui/component.ProgressBar.renderTextBar({{{{0xccc0a0, 0x401, {...}, 0x1, {...}, {...}, 0x0, 0x0, 0x0, 0x0, ...}, ...}, ...}}, ...)
        /home/brecht/IdeaProjects/k8s/jenkins-tui/internal/tui/component/progressbar.go:212 +0xd6e
github.com/brecht/jenkins-tui/internal/tui/component.ProgressBar.renderBarInner({{{{0xccc0a0, 0x401, {...}, 0x1, {...}, {...}, 0x0, 0x0, 0x0, 0x0, ...}, ...}, ...}}, ...)
        /home/brecht/IdeaProjects/k8s/jenkins-tui/internal/tui/component/progressbar.go:142 +0x625
github.com/brecht/jenkins-tui/internal/tui/component.ProgressBar.DualRenderWithText({{{{0xccc0a0, 0x401, {...}, 0x1, {...}, {...}, 0x0, 0x0, 0x0, 0x0, ...}, ...}, ...}}, ...)
        /home/brecht/IdeaProjects/k8s/jenkins-tui/internal/tui/component/progressbar.go:88 +0xf9
github.com/brecht/jenkins-tui/internal/tui/view.(*BuildList).populateTable(0xc000476000)
        /home/brecht/IdeaProjects/k8s/jenkins-tui/internal/tui/view/buildlist.go:164 +0x3ec
github.com/brecht/jenkins-tui/internal/tui/view.(*BuildList).Update(0xc000476000, {0x8365a0?, 0xc0001b4780?})
        /home/brecht/IdeaProjects/k8s/jenkins-tui/internal/tui/view/buildlist.go:200 +0x408
github.com/brecht/jenkins-tui/internal/tui.App.Update({{{{0xccc0a0, 0x401, {...}, 0x1, {...}, {...}, 0x0, 0x0, 0x0, 0x0, ...}, ...}, ...}, ...}, ...)
        /home/brecht/IdeaProjects/k8s/jenkins-tui/internal/tui/app.go:218 +0xc82
github.com/charmbracelet/bubbletea.(*Program).eventLoop(0xc0003b8000, {0x9c1a00?, 0xc000146000?}, 0xc00009e1c0)
        /home/brecht/go/pkg/mod/github.com/charmbracelet/bubbletea@v1.3.10/tea.go:494 +0x84d
github.com/charmbracelet/bubbletea.(*Program).Run(0xc0003b8000)
        /home/brecht/go/pkg/mod/github.com/charmbracelet/bubbletea@v1.3.10/tea.go:716 +0xb56
main.main()
        /home/brecht/IdeaProjects/k8s/jenkins-tui/cmd/jenkins-tui/main.go:47 +0x7ea
Error running TUI: program was killed: program experienced a panic
exit status 1
```

2. When navigating with shortcuts (like `s` later described, or going from a running build to a stage view or logs), the reference (e.g. `spring-security-oauth-demo/main/#64`) is often fucked up and when hitting escape,
   we sometimes don't go back up to where this reference would suggest, as instead we end up where we came from when hitting the shortcut.
   This indicates a fundamental abstraction failure and hacky patchwork. This system needs some foundational rework.
3. In some cases, the auto-advance of the stage view doesn't work. It remains on a Succes option. Which edge cases exist that may cause this? I think race condition? Make this more robust.
4. When scanning a folder, this isn't shown as a running pipeline. Is this expected behavior of the API, or abstraction failure on our end?
5. The `trigger build` popup can be escaped with ESC, but this key is also captured by the view, returning us back one level. Let's separate those into individual keypresses (i.e. ESC only closes popups first).
6. When scrolling through stages in the stage view by holding down an arrow key, the UI bogs down. It seems the network requests bog down the UI thread. Investigate where and how we can use async throughout the app.
7. References like `git/dov/dov-app-website` and `git/indicatorenwebsite` are not truncated to after the last `/`. Let's always do that, not just when the name is too long (be consistent).

Changes to existing behavior
1. In the jobs view, currently it lists status of the main branch and its last run. It's not clear from the table that this is the main branch; one would think it's the last run.
   It would also be useful to have the last build listed there as well. Maybe list main branch stable/unstable (call it differently) and last build status + timing?
   When multiple branches of this job run, good idea to just indicate `2 running`?
   Could this be extended to show how many builds are ran in the folders?
2. When opening the stage view, the last non-skipped stage is selected. This makes hitting the `f` redundant. Let's remove this, and have `s` to open the stages view instead.
   When selecting a MultiBranch project, `s` opens the stage view of the build that has started last.
   When selecting a Folder, `s` opens the stage view of the build that has started last of all projects.
   When selecting a branch or MR, `s` open the stage view of the last build.
   In the builds view, `s` is the same as `enter`
   Similarly, let `l` in these places open the full logs of the last build.
3. When in the stages view or stages log view, as long as we stay inside one of these two, cache the stage logs nodes that have a finished status. No need to re-fetch those each time.
   Since there is lots of data that becomes effectively immutable after a certain moment, perhaps there is a case to be made for a generalized cache (with a cleanup mechanism)?
4. Change the running builds from `b` to `r`
5. In stage logs view, disable the command `p`; this is only relevant for the full logs.
6. In the stage view, how would we handle a failed build but all stages show success? I.e. a syntax error outside a stage. Design wise, this isn't cleanly supported. What do you propose?
7. In the reference `spring-security-oauth-demo/feature/myfeature/#64`, it's not visually distinct that `feature/myfeature` is the branch since between the job `spring-security-oauth-demo` and the branch is also a forward slash.
   How would we solve this? Colors? Is there idiomatic symbolism to indicate this?
8. Add `w` to wrap logs to the stage view preview logs.

New stuff
1. Add a new view to show pipelines triggered by the user (include push events from git and whatnot). Let this list be a hotlist of a set number of items, ordered by recency. Include running and completed builds.
2. Add colorblind modes that change certain colors of existing themes (a translation layer between the app and the theme that auto-adjuts colors)
3. Give the terminal window a title. Perhaps something dynamic?
4. What commands would be useful? We already have keybindings for all our functions, what use do commands still have? One we definitely need is to trigger a build with parameters.
5. Describe! This shows the user the pipeline code. With `e`, the user can edit the pipeline in `vi` and run it in place (like we can with the GUI).
6. Depends on the describe feature; add a new view that lists jobs on the main branch and which jenkins pipeline library they use. It could bump library versions in bulk to test if library versions could be upgraded.
   Could it also make an MR to persist this change? Or call a custom pipeline that applies this change.
7. K9s logs can scroll horizontally when warping is off. Can we have this too?
8. Can we see queued builds as well? Add a number like we have for running builds in the header. In this view, let users cancel queued builds we well.
9. Log error/warning highlighting. Use f2 / shift f2 to jump to the next / previous one like in the IntelliJ IDEA. Add an icon to the top right of log views insicating the amount of errors and warnings.
10. Context switching between Jenkins controllers (`context` / `ctx` command)
11. In the builds view; add a way to compare the diff between two build numbers. Argonaut uses git-diff for this, I propose to do the same. This would use a command like `diff #13 #14`. Or add a way that allows for selecting multiple items in a list view.
12. Integrate testing; show how many passed/failed, and/or visualize this nicely in the builds view. Is this information also useful somewhere else?
    Worth it to create it tests page that shows the information that Jenkins displays on its tests webpage? I.e. tests in a tree view per package, showing failed/skipped/passed/total/duration
13. Build artifacts would be nice, but we don't currently use those.
14. Navigating by stage (i.e. filter pipelines that contain a similarly named stage). Could be useful to see for example all pipelines using Trivy scans.
15. Orphan & zombie hunter; list jobs that haven't been run in 3 months, or branch pipelines that don't exist in git any more.
16. SSO
17. Terminal notifications
18. Stage view; show stage names of a previous run grayed out and unselectable, so that the user knows how many to expect. If the current stage names don't match the expected ones, these grayed out names are removed. Also in gray, show the previous duration of the sage.
    If we're in the mode where the grayed out names are matching, we can use the timings of the stages to update the main progress bar more accurately (could be that a stage is faster because of docker layer cache). When new stages don't match the previous stage names, fall back to the original build timing estimate.
    Wait, could we show progress bars on stage levels then as well? Would be SUPER nice!
19. In the builds view, pres `p` to open the pending stages view (same as where you trigger a new build), this so that a dev doesn't need to wait in the builds view on the build and press enter
20. The main logs can say 'Stage "Git tag" skipped due to when conditional'. This text is outside of our stages logs. Do we have this information somewhere? Would be REALLY nice if we could have this data and use it to mark the stage as Skipped, even though Jenkins reports it as Success.
    Absolute worst case could be parsing the main logs in the background (though this data should be cached somewhere anyway)
21. Open stageview of #last instead of #number, to keep tracking the last pipeline run without having to go to the builds menu

Stuff for human to describe further
- `stages(metadata-service-config/MR-24/#11)` build failed maar geen stage failed