package widget

// ScrollMarkerKind identifies the type of a scrollbar marker.
type ScrollMarkerKind uint8

const (
	ScrollMarkerSearch  ScrollMarkerKind = iota // search match
	ScrollMarkerWarning                         // warning line
	ScrollMarkerError                           // error line
)

// ScrollMarker marks a notable display-line position on the scrollbar.
type ScrollMarker struct {
	Line int
	Kind ScrollMarkerKind
}

// ScrollInfo describes the scroll position of a scrollable view.
type ScrollInfo struct {
	Offset     int
	TotalLines int
	ViewHeight int
	Markers    []ScrollMarker
}
