//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include <signal.h>
#include <unistd.h>
#import <Cocoa/Cocoa.h>

@interface SlimferenceIndicatorView : NSView {
	NSString *_label;
}
- (instancetype)initWithFrame:(NSRect)frame label:(NSString *)label;
@end

@implementation SlimferenceIndicatorView
- (instancetype)initWithFrame:(NSRect)frame label:(NSString *)label {
	self = [super initWithFrame:frame];
	if (self) {
		_label = [label copy];
	}
	return self;
}

- (void)drawRect:(NSRect)dirtyRect {
	(void)dirtyRect;
	NSRect bounds = [self bounds];
	NSBezierPath *pill = [NSBezierPath bezierPathWithRoundedRect:bounds xRadius:13 yRadius:13];
	[[NSColor colorWithCalibratedRed:0.055 green:0.062 blue:0.075 alpha:0.90] setFill];
	[pill fill];
	[[NSColor colorWithCalibratedRed:0.35 green:0.95 blue:0.58 alpha:0.55] setStroke];
	[pill setLineWidth:1.0];
	[pill stroke];

	NSBezierPath *dot = [NSBezierPath bezierPathWithOvalInRect:NSMakeRect(12, 10, 10, 10)];
	[[NSColor colorWithCalibratedRed:0.35 green:1.0 blue:0.62 alpha:1.0] setFill];
	[dot fill];

	NSDictionary *attrs = @{
		NSFontAttributeName: [NSFont monospacedSystemFontOfSize:12 weight:NSFontWeightSemibold],
		NSForegroundColorAttributeName: [NSColor colorWithCalibratedRed:0.91 green:0.97 blue:0.94 alpha:1.0],
		NSKernAttributeName: @0.4
	};
	[_label drawAtPoint:NSMakePoint(30, 8) withAttributes:attrs];
}
@end

@interface SlimferenceIndicatorDelegate : NSObject <NSApplicationDelegate> {
	int _watchPID;
}
- (instancetype)initWithWatchPID:(int)pid;
- (void)checkWatchPID:(NSTimer *)timer;
@end

@implementation SlimferenceIndicatorDelegate
- (instancetype)initWithWatchPID:(int)pid {
	self = [super init];
	if (self) {
		_watchPID = pid;
	}
	return self;
}

- (void)checkWatchPID:(NSTimer *)timer {
	(void)timer;
	if (_watchPID > 0 && kill(_watchPID, 0) != 0) {
		[NSApp terminate:nil];
	}
}
@end

static void SlimferenceIndicatorRun(const char *labelCString, int watchPID) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];

		NSString *label = [NSString stringWithUTF8String:labelCString];
		if (label == nil || [label length] == 0) {
			label = @"SLIMFERENCE ACTIVE";
		}

		NSScreen *screen = [NSScreen mainScreen];
		NSRect visible = [screen visibleFrame];
		CGFloat width = 186.0;
		CGFloat height = 30.0;
		NSRect frame = NSMakeRect(
			NSMaxX(visible) - width - 24.0,
			NSMaxY(visible) - height - 24.0,
			width,
			height
		);

		NSPanel *panel = [[NSPanel alloc]
			initWithContentRect:frame
			styleMask:NSWindowStyleMaskBorderless
			backing:NSBackingStoreBuffered
			defer:NO
		];
		[panel setReleasedWhenClosed:NO];
		[panel setOpaque:NO];
		[panel setBackgroundColor:[NSColor clearColor]];
		[panel setLevel:NSStatusWindowLevel];
		[panel setIgnoresMouseEvents:YES];
		[panel setCanHide:NO];
		[panel setCollectionBehavior:
			NSWindowCollectionBehaviorCanJoinAllSpaces |
			NSWindowCollectionBehaviorFullScreenAuxiliary |
			NSWindowCollectionBehaviorStationary
		];

		SlimferenceIndicatorView *view = [[SlimferenceIndicatorView alloc] initWithFrame:NSMakeRect(0, 0, width, height) label:label];
		[panel setContentView:view];
		[panel orderFrontRegardless];

		SlimferenceIndicatorDelegate *delegate = [[SlimferenceIndicatorDelegate alloc] initWithWatchPID:watchPID];
		[NSApp setDelegate:delegate];
		if (watchPID > 0) {
			[NSTimer scheduledTimerWithTimeInterval:1.0 target:delegate selector:@selector(checkWatchPID:) userInfo:nil repeats:YES];
		}
		[NSApp run];
	}
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

func init() {
	// AppKit requires windows on the original process main thread.
	runtime.LockOSThread()
}

func codexDesktopIndicatorSupported() bool {
	return true
}

func runCodexDesktopIndicatorWindow(label string, watchPID int) error {
	cLabel := C.CString(label)
	defer C.free(unsafe.Pointer(cLabel))
	C.SlimferenceIndicatorRun(cLabel, C.int(watchPID))
	return nil
}
