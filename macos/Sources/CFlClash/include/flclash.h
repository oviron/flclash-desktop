// The two functions of the Go engine library; see bridge/bridge.go.
#ifndef FLCLASH_H
#define FLCLASH_H

// Answers one JSON request with a JSON reply. Free the reply with FlcFree.
char *FlcCall(char *input);
void FlcFree(char *p);

#endif
