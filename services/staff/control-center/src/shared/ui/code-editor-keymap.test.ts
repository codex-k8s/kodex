import { history, undo, undoDepth } from "@codemirror/commands";
import {
  EditorState,
  Transaction,
  type TransactionSpec,
} from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { describe, expect, it, vi } from "vitest";
import { insertVoiceText } from "./code-editor-keymap";

describe("Вставка голосового текста CodeMirror", () => {
  it("заменяет selection отдельно от предшествующего и следующего ввода", () => {
    const view = {
      state: EditorState.create({ doc: "start end", extensions: [history()] }),
      scrollDOM: { scrollTop: 240, scrollLeft: 32 },
      focus: vi.fn(),
      dispatch(spec: TransactionSpec | Transaction) {
        this.state =
          spec instanceof Transaction
            ? spec.state
            : this.state.update(spec).state;
      },
    };
    view.dispatch({
      changes: { from: 0, insert: "a" },
      selection: { anchor: 7, head: 10 },
      annotations: Transaction.userEvent.of("input.type"),
    });
    insertVoiceText(view as unknown as EditorView, "voice");
    expect(view.state.doc.toString()).toBe("astart voice");
    expect(view.state.selection.main.head).toBe(12);
    expect(view.focus).toHaveBeenCalledOnce();
    expect(view.scrollDOM).toEqual({ scrollTop: 240, scrollLeft: 32 });
    view.dispatch({
      ...view.state.replaceSelection("z"),
      annotations: Transaction.userEvent.of("input.type"),
    });
    expect(undoDepth(view.state)).toBe(3);
    const target = {
      get state() {
        return view.state;
      },
      dispatch: (transaction: Transaction) => view.dispatch(transaction),
    };
    expect(undo(target)).toBe(true);
    expect(view.state.doc.toString()).toBe("astart voice");
    expect(undo(target)).toBe(true);
    expect(view.state.doc.toString()).toBe("astart end");
    expect(undo(target)).toBe(true);
    expect(view.state.doc.toString()).toBe("start end");
  });
  it.each([EditorState.readOnly.of(true), EditorView.editable.of(false)])(
    "не изменяет заблокированный редактор",
    (extension) => {
      const view = {
        state: EditorState.create({ doc: "original", extensions: [extension] }),
        dispatch: vi.fn(),
      };
      insertVoiceText(view as unknown as EditorView, "voice");
      expect(view.dispatch).not.toHaveBeenCalled();
    },
  );
});
