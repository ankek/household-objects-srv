package main

import (
	"fmt"
	"math/rand"
	"strings"
)

var adjectives = []string{
	"blue", "compact", "vintage", "spare", "heavy", "portable", "insulated",
	"stainless", "wooden", "folding", "cordless", "rechargeable", "waterproof",
	"adjustable", "ceramic", "magnetic", "telescopic", "reinforced", "quiet",
}

var materials = []string{
	"oak", "aluminium", "polycarbonate", "cotton", "silicone", "brass",
	"titanium", "bamboo", "neoprene", "polypropylene", "leather", "glass",
}

var nouns = []string{
	"drill", "kettle", "ladder", "hose", "toolbox", "lamp", "router", "blender",
	"jigsaw", "vacuum", "printer", "chair", "monitor", "speaker", "thermometer",
	"extension cord", "socket set", "paint roller", "hedge trimmer", "sewing kit",
	"tent", "sleeping bag", "cooler box", "bicycle pump", "first aid kit",
	"hard drive", "camera", "tripod", "microphone", "keyboard", "air fryer",
}

var brands = []string{
	"Norhalt", "Kessner", "Vantorp", "Brimmel", "Draywood", "Olsberg", "Tallgren",
	"Rivenhall", "Sundqvist", "Marlowe", "Pikstone", "Ferrendale",
}

var roomNames = []string{
	"Garage", "Loft", "Basement", "Kitchen", "Utility Room", "Garden Shed",
	"Study", "Hallway Cupboard", "Attic", "Workshop", "Pantry", "Car Boot",
}

var containerNames = []string{
	"Shelf A", "Shelf B", "Blue Crate", "Metal Cabinet", "Top Drawer",
	"Bottom Drawer", "Wall Rack", "Pegboard", "Storage Bin", "Toolchest",
}

var labelNames = []string{
	"power tools", "hand tools", "camping", "electronics", "kitchen",
	"seasonal", "consumable", "fragile", "loaned out", "warranty active",
	"needs repair", "spare parts", "cables", "photography", "gardening",
	"cleaning", "safety", "bulk", "archive", "insured", "rarely used",
	"office", "automotive", "hobby", "sports",
}

var descriptionPhrases = []string{
	"kept in the original packaging",
	"purchased during the spring sale",
	"missing one of the mounting brackets",
	"battery holds roughly four hours of charge",
	"replaces the unit that failed last winter",
	"manual is filed with the receipts",
	"only used a handful of times",
	"needs a replacement filter every six months",
	"shared with the neighbours occasionally",
	"rated for outdoor use in wet conditions",
	"serial number is engraved on the underside",
	"came as part of a two piece set",
	"stored alongside the matching charger",
	"warranty runs out at the end of next year",
	"the plug was rewired after the fuse blew",
	"fits the standard rail mount",
	"noticeably heavier than the older model",
	"colour has faded from sun exposure",
}

var identificationKinds = []string{"serial", "model", "asset_tag", "barcode", "other"}

var noteFragments = []string{
	"collected in person from the depot",
	"invoice covers two units",
	"price included the extended cover",
	"delivered with a damaged outer box",
	"paid by card, statement reference retained",
	"seller offered a discount for cash",
}

var searchTerms = func() []string {
	terms := make([]string, 0, len(nouns)+len(adjectives)+len(brands)+len(materials))
	for _, n := range nouns {
		fields := strings.Fields(n)
		terms = append(terms, fields[len(fields)-1])
	}
	terms = append(terms, adjectives...)
	terms = append(terms, materials...)
	for _, b := range brands {
		terms = append(terms, strings.ToLower(b))
	}
	return terms
}()

func pick[T any](r *rand.Rand, xs []T) T {
	return xs[r.Intn(len(xs))]
}

func itemName(r *rand.Rand) string {
	return fmt.Sprintf("%s %s %s", pick(r, brands), pick(r, adjectives), pick(r, nouns))
}

func itemDescription(r *rand.Rand) string {
	n := 2 + r.Intn(3)
	parts := make([]string, 0, n+1)
	parts = append(parts, fmt.Sprintf("%s %s unit", pick(r, materials), pick(r, adjectives)))
	for i := 0; i < n; i++ {
		parts = append(parts, pick(r, descriptionPhrases))
	}
	return strings.Join(parts, "; ")
}

func identificationValue(r *rand.Rand, kind string) string {
	switch kind {
	case "barcode":
		return fmt.Sprintf("%013d", r.Int63n(1e13))
	case "model":
		return fmt.Sprintf("%s-%d%c", strings.ToUpper(pick(r, brands)[:3]), 100+r.Intn(900), 'A'+rune(r.Intn(26)))
	case "asset_tag":
		return fmt.Sprintf("HHO-%05d", r.Intn(100000))
	default:
		return fmt.Sprintf("SN%09d", r.Int63n(1e9))
	}
}

func purchaseNote(r *rand.Rand) string {
	return pick(r, noteFragments)
}

func shortCode(n int) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	if n == 0 {
		return "A"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{alphabet[n%len(alphabet)]}, b...)
		n /= len(alphabet)
	}
	return string(b)
}
