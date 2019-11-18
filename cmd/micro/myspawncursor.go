package main

// Experimental multi cursor creation from https://github.com/zyedidia/micro/pull/1416.

// SpawnMultiCursorUp creates additional cursor, at the same X (if possible), one Y less.
func (v *View) SpawnMultiCursorUp(usePlugin bool) bool {

	if usePlugin && !PreActionCall("SpawnMultiCursorUp", v) {
		return false
	}

	if v.Cursor.Y == 0 {
		return false
	}

	v.Cursor.GotoLoc(Loc{v.Cursor.X, v.Cursor.Y - 1})
	v.Cursor.Relocate()

	if v.mainCursor() {
		c := &Cursor{
			buf: v.Buf,
		}
		c.GotoLoc(Loc{v.Cursor.X, v.Cursor.Y + 1})
		v.Buf.cursors = append(v.Buf.cursors, c)
	}

	v.Buf.MergeCursors()
	v.Buf.UpdateCursors()

	if usePlugin {
		PostActionCall("SpawnMultiCursorUp", v)
	}

	return false
}

// SpawnMultiCursorDown creates additional cursor, at the same X (if possible), one Y more.
func (v *View) SpawnMultiCursorDown(usePlugin bool) bool {

	if usePlugin && !PreActionCall("SpawnMultiCursorDown", v) {
		return false
	}

	if v.Cursor.Y+1 == v.Buf.LinesNum() {
		return false
	}

	v.Cursor.GotoLoc(Loc{v.Cursor.X, v.Cursor.Y + 1})
	v.Cursor.Relocate()

	if v.mainCursor() {
		c := &Cursor{
			buf: v.Buf,
		}
		c.GotoLoc(Loc{v.Cursor.X, v.Cursor.Y - 1})
		v.Buf.cursors = append(v.Buf.cursors, c)
	}

	v.Buf.MergeCursors()
	v.Buf.UpdateCursors()

	if usePlugin {
		PostActionCall("SpawnMultiCursorDown", v)
	}

	return false
}
