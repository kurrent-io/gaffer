import type {
  ChainWithPartitionBy,
  FilterByFn,
  ForeachStreamFn,
  FromAllChain,
  FromCategoryChain,
  FromStreamChain,
  OutputStateChain,
  OutputStateFn,
  OutputToFn,
  PartitionByChain,
  PartitionByFn,
  TransformationChain,
  TransformByFn,
  WhenChain,
  WhenFn,
} from "./chains.ts";
import type {
  FromAllFn,
  FromCategoriesFn,
  FromCategoryFn,
  FromStreamFn,
  FromStreamsFn,
  OnAnyFn,
  OnEventFn,
  OptionsFn,
} from "./definition-functions.ts";
import type {
  EventBody,
  EventMetadata,
  KurrentEvent,
} from "./events.ts";
import type { Handlers } from "./handlers.ts";
import type { ProjectionOptions } from "./options.ts";
import type {
  CopyToFn,
  EmitFn,
  LinkStreamToFn,
  LinkToFn,
  LogFn,
} from "./runtime-functions.ts";

declare global {
  const options: OptionsFn;
  const fromStream: FromStreamFn;
  const fromCategory: FromCategoryFn;
  const fromCategories: FromCategoriesFn;
  const fromAll: FromAllFn;
  const fromStreams: FromStreamsFn;
  const on_event: OnEventFn;
  const on_any: OnAnyFn;
  const log: LogFn;
  const emit: EmitFn;
  const linkTo: LinkToFn;
  const linkStreamTo: LinkStreamToFn;
  const copyTo: CopyToFn;

  namespace Projection {
    export type {
      ChainWithPartitionBy,
      EventBody,
      EventMetadata,
      FilterByFn,
      ForeachStreamFn,
      FromAllChain,
      FromCategoryChain,
      FromStreamChain,
      Handlers,
      KurrentEvent,
      OutputStateChain,
      OutputStateFn,
      OutputToFn,
      PartitionByChain,
      PartitionByFn,
      ProjectionOptions,
      TransformationChain,
      TransformByFn,
      WhenChain,
      WhenFn,
    };
  }
}
