// Schemas for panel-initiated DAP custom requests. The request is
// issued by panels/state.ts; the response is DAP wire format but the
// only consumer is the panel itself, so the schema lives here.

import * as v from "valibot";

export const PartitionStateResponseSchema = v.object({
	state: v.optional(v.unknown()),
	result: v.optional(v.unknown()),
});
export type PartitionStateResponse = v.InferOutput<
	typeof PartitionStateResponseSchema
>;
