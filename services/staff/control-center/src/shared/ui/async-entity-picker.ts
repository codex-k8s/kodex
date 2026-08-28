export interface AsyncEntityOption {
  ref: string;
  title: string;
  description?: string;
  meta?: string;
  disabled?: boolean;
  disabledReason?: string;
}

export interface AsyncEntityPage {
  items: AsyncEntityOption[];
  nextPageToken?: string;
}
