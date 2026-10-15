// Shared C declarations for the event decoder (see event.go).

// ly_event is a flat, pointer-free mirror of the fields of every core X11
// event. cgo cannot expose the members of Xlib's anonymous union, so the
// conversion happens once in C and the Go side only sees plain fields.
typedef struct {
	int           type;
	int           serial;
	int           send_event;
	unsigned long time;
	unsigned long root;
	unsigned long window;
	unsigned long child;      // subwindow / parent, depending on the event
	int           x, y;        // coordinates relative to window
	int           root_x, root_y; // coordinates relative to root
	unsigned long state;
	int           same_screen;
	int           width, height, border_width;
	unsigned long above_sibling;
	int           detail;
	unsigned long value_mask;
	unsigned long atom;
	unsigned long owner, requestor, selection, target, property;
	unsigned long msg_type;
	int           format;
	unsigned long data[5];
	int           override_redirect;
	int           from_configure;
	int           mode;
	unsigned long focus;
	unsigned long count;
	unsigned long parent;
	unsigned long extension;
	unsigned long request;
	unsigned long resource;
	unsigned long minor;
} ly_event;
