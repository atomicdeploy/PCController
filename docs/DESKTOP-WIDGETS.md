# Desktop widgets and compact mode

Open the desktop/tray context menu and choose **Desktop widgets…**. Toggle the
front panel, live charts, or any channel to open it in its own acrylic window.
Checked items are visible; choosing one again hides it. Closing a widget hides
that widget, while **Close desktop** exits the whole desktop interface.

Each widget keeps its position, size and visibility across launches. The front
panel and charts restore before a board connects. A channel restores when its
channel identity appears in the host catalog; saved names do not identify it.
Renaming a channel updates its detached window title as well as its card.

Enable **Magnetic window snapping** in the same menu. Drag a window by its
titlebar near another widget or a monitor edge to align their edges into a grid.
Hold Ctrl during the drag to bypass snapping. New desktop configurations enable
snapping; existing saved preferences are preserved.

Choose **Compact front panel** from the desktop/tray menu to show just the
seven-segment display and its four controls. Selecting Channels or Preferences
expands the main window before opening that page. Detached widgets remain
independently visible in either main-window mode.

Appearance choices apply to all widget windows, including acrylic, theme,
raised/flat controls and Windows/macOS chrome. Windows accessibility and
transparency preferences retain their normal solid fallback.

The windows share the existing host connection and its latest observations.
Hardware actions retain the same connection, lock and emergency-stop checks.
Opening a widget does not create another board connection. Charts use bounded
in-memory samples and retain their last values with an Offline label when the
connection is lost.

Chart strokes use subpixel vector antialiasing, rounded joins and DPI-scaled
widths. Missing samples break the line. Raster images are reused between data
updates and released when a widget is hidden.

The [README gallery](../README.md) shows
offline sample renders. Regenerate it using
`node Tools/Build/update-readme-previews.mjs` on Windows.
