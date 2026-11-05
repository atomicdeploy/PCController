// Package desktop exposes the imported Windows presentation stack without
// exporting its donor lamp protocol or creating another hardware owner.
package desktop

import "rayanlamp.local/app/presentation"

type Snapshot = presentation.Snapshot
type Options = presentation.Options
type Channel = presentation.Channel
type Action = presentation.Action
type Page = presentation.Page
type Field = presentation.Field
type PageAction = presentation.PageAction
