import {
  indentWithTab,
  isolateHistory,
  toggleTabFocusMode,
} from "@codemirror/commands";
import { keymap, EditorView } from "@codemirror/view";
import { EditorState, Transaction } from "@codemirror/state";

export const codeEditorKeymap = keymap.of([
  indentWithTab,
  { key: "Ctrl-m", mac: "Ctrl-m", run: toggleTabFocusMode },
]);

export function insertVoiceText(
  view: EditorView | undefined,
  text: string,
): void {
  if (
    !view ||
    view.state.facet(EditorState.readOnly) ||
    !view.state.facet(EditorView.editable)
  )
    return;
  const scrollTop = view.scrollDOM.scrollTop;
  const scrollLeft = view.scrollDOM.scrollLeft;
  view.dispatch({
    ...view.state.replaceSelection(text),
    annotations: [
      Transaction.userEvent.of("input.voice"),
      isolateHistory.of("full"),
    ],
  });
  view.focus();
  view.scrollDOM.scrollTop = scrollTop;
  view.scrollDOM.scrollLeft = scrollLeft;
}
