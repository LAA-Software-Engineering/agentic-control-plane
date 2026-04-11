// Package models abstracts model providers and client interfaces.
//
// [Registry] resolves namespace/model_id strings using Project.spec.providers.models.
// Use [MockClient] for deterministic tests; [OpenAIClient] is the MVP OpenAI-compatible backend (§12.2 F).
package models
