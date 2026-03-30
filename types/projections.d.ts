import type {
  ChainWithPartitionBy,
  FilterByFn,
  ForeachStreamFn,
  FromAllChain,
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
} from "./types/chains.ts";
import type {
  FromAllFn,
  FromCategoriesFn,
  FromCategoryFn,
  FromStreamFn,
  FromStreamsFn,
  OptionsFn,
} from "./types/definition-functions.ts";
import type {
  EventBody,
  EventMetadata,
  KurrentEvent,
  LinkMetadata,
  StreamLinkMetadata,
} from "./types/events.ts";
import type { Handlers } from "./types/handlers.ts";
import type { ProjectionOptions } from "./types/options.ts";
import {
  CopyToFn,
  EmitFn,
  LinkStreamToFn,
  LinkToFn,
  LogFn,
} from "./types/runtime-functions.ts";

declare global {
  /**
   * see {@link OptionsFn}
   */
  const options: OptionsFn;

  /**
   * see {@link FromStreamFn}
   */
  const fromStream: FromStreamFn;

  /**
   * see {@link FromStreamFn}
   */
  const fromCategory: FromCategoryFn;

  /**
   * see {@link FromCategoriesFn}
   */
  const fromCategories: FromCategoriesFn;

  /**
   * see {@link FromAllFn}
   */
  const fromAll: FromAllFn;

  /**
   * see {@link FromStreamsFn}
   */
  const fromStreams: FromStreamsFn;

  /**
   * see {@link LogFn}
   */
  const log: LogFn;

  /**
   * see {@link EmitFn}
   */
  const emit: EmitFn;

  /**
   * see {@link LinkToFn}
   */
  const linkTo: LinkToFn;

  /**
   * see {@link LinkStreamToFn}
   */
  const linkStreamTo: LinkStreamToFn;

  /**
   * see {@link CopyToFn}
   */
  const copyTo: CopyToFn;

  namespace Projection {
    /**
     * Types and functions for working with projections in Kurrent.
     */
    export type {
      ChainWithPartitionBy,
      EventBody,
      EventMetadata,
      FilterByFn,
      ForeachStreamFn,
      FromAllChain,
      FromStreamChain,
      Handlers,
      KurrentEvent,
      LinkMetadata,
      OutputStateChain,
      OutputStateFn,
      OutputToFn,
      PartitionByChain,
      PartitionByFn,
      ProjectionOptions,
      StreamLinkMetadata,
      TransformationChain,
      TransformByFn,
      WhenChain,
      WhenFn,
    };
  }
}
