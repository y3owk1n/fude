# gui

The native window fude opens with `fude --gui`. It draws a session in
the system's own window: AppKit on macOS, GTK 4 on Linux and Win32 with
Direct2D and DirectWrite on Windows. It calls each system through
[purego](https://github.com/ebitengine/purego) and `syscall`, so it needs
no cgo and no C compiler.

```go
e, err := editor.Start(os.Args[1:])
if err != nil {
	return err
}
return gui.Run(e)
```

`Run` takes any [`client.Session`](../frontend/client), as
[`tui`](../tui) does, and shows it until the session ends. Call it from
the main goroutine. The package locks that goroutine to the main thread,
which each system's window needs.

The text is cells from [`compose`](../frontend/compose). The title bar,
splits, the lists of the palette, the finder and menus, and the status
bars on macOS and Linux are the system's own controls. They show the
session and take the mouse, and every key goes to the session.

## License

[MIT](../LICENSE)
