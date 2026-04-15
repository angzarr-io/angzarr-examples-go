// Hand domain upcaster — transforms old event versions to current during replay.
//
// The upcaster is generic: parameterized by domain name, not tied to hand
// specifically. The same pattern applies to any domain's schema evolution.
//
// Currently passthrough — register transformation functions as schema changes:
//
//	router.On("examples.CardsDealtV1", upcastCardsDealtV1)
package main

import (
	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
)

func buildRouter() *angzarr.UpcasterRouter {
	return angzarr.NewUpcasterRouter("hand")
	// Register transformations as schema evolves:
	// .On("examples.CardsDealtV1", upcastCardsDealtV1)
}

func handleUpcast(events []*pb.EventPage) []*pb.EventPage {
	router := buildRouter()
	return router.Upcast(events)
}

func main() {
	handler := angzarr.NewUpcasterGrpcHandler("upcaster-hand", "hand").
		WithHandle(handleUpcast)
	angzarr.RunUpcasterServer("upcaster-hand", "50402", handler)
}
