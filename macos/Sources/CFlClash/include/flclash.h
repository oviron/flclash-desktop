// The functions of the Go engine library; see bridge/bridge.go.
#ifndef FLCLASH_H
#define FLCLASH_H

// Receives an event of the model as JSON; the text lives until it returns.
typedef void (*FlcEmit)(const char *event);

// Starts the model in the given language; emit hears every event, off the
// main thread.
void FlcStart(char *lang, FlcEmit emit);
// Hands the model one message as JSON; it does not wait.
void FlcSend(char *message);
// Turns off what runs, as the app quits; returns once the system is as it was.
void FlcStop(void);

#endif
