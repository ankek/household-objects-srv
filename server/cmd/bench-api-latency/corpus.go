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
	"tent", "cooler", "pump", "camera", "tripod", "microphone", "keyboard", "fryer",
}

var brands = []string{
	"Norhalt", "Kessner", "Vantorp", "Brimmel", "Draywood", "Olsberg", "Tallgren",
	"Rivenhall", "Sundqvist", "Marlowe", "Pikstone", "Ferrendale",
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
	"fits the standard rail mount",
	"noticeably heavier than the older model",
	"colour has faded from sun exposure",
}

var searchTerms = func() []string {
	seen := make(map[string]struct{}, len(nouns)+len(adjectives)+len(materials)+len(brands))
	var terms []string
	add := func(s string) {
		s = strings.ToLower(s)
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		terms = append(terms, s)
	}
	for _, n := range nouns {
		add(n)
	}
	for _, a := range adjectives {
		add(a)
	}
	for _, m := range materials {
		add(m)
	}
	for _, b := range brands {
		add(b)
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
	n := 2 + r.Intn(2)
	parts := make([]string, 0, n+1)
	parts = append(parts, fmt.Sprintf("%s %s unit", pick(r, materials), pick(r, adjectives)))
	for range n {
		parts = append(parts, pick(r, descriptionPhrases))
	}
	return strings.Join(parts, "; ")
}
