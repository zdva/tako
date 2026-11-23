# pty

A copy of `plugins/terminal/internal/pty` from MyGo v0.10.0
(github.com/egoist/mygo, MIT, see LICENSE.mygo), unchanged except for this
note and the package doc. MyGo's terminal starts local programs with it; Go's
internal-package rule keeps Tako from importing it, and Tako needs to own the
pseudo-terminal of a local tab so that what is typed there can join input
sync (internal/localpty). Keeping the same code keeps local terminals
behaving as MyGo's did: the same login shell, environment, resizing and
hang-up, and ConPTY on Windows.

When MyGo is upgraded, compare with its copy and update this one.
