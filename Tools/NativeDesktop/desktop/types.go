// Package desktop exposes the imported Windows presentation stack without
// exporting its donor device protocol or creating another hardware owner.
package desktop

import "pccontroller.local/desktop/presentation"

type Snapshot = presentation.Snapshot
type Options = presentation.Options
type Channel = presentation.Channel
type Action = presentation.Action
type Page = presentation.Page
type Field = presentation.Field
type PageAction = presentation.PageAction
type MenuItem = presentation.MenuItem
type Message = presentation.Message
type RFReceipt = presentation.RFReceipt
type Seat = presentation.Seat
type Preferences = presentation.Preferences
type MotionHoldLease = presentation.MotionHoldLease
type ControlOwner = presentation.ControlOwner
