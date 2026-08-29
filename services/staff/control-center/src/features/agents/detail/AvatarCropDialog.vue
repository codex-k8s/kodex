<script setup lang="ts">
import { Crop, Move } from "@lucide/vue";
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";

import {
  avatarCropTransform,
  avatarMaximumDimension,
  avatarOutputSize,
  type AvatarCropOffset,
} from "@/features/agents/detail/avatar";
import { agentDetailCopy } from "@/features/agents/detail/copy";
import ModalDialog from "@/shared/ui/ModalDialog.vue";

const props = defineProps<{ file: File; busy: boolean }>();
const emit = defineEmits<{ close: []; confirm: [file: File] }>();
const { locale } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value).avatar);
const canvas = ref<HTMLCanvasElement>();
const image = new Image();
const objectUrl = ref("");
const ready = ref(false);
const problem = ref("");
const zoom = ref(1);
const offset = ref<AvatarCropOffset>({ x: 0, y: 0 });
const dragging = ref(false);
let dragStart = { clientX: 0, clientY: 0, offsetX: 0, offsetY: 0 };

function draw(): void {
  const target = canvas.value;
  if (!target || !ready.value) return;
  const context = target.getContext("2d", { alpha: false });
  if (!context) {
    problem.value = copy.value.processingError;
    return;
  }
  const transform = avatarCropTransform(
    image.naturalWidth,
    image.naturalHeight,
    avatarOutputSize,
    zoom.value,
    offset.value,
  );
  offset.value = { x: transform.x, y: transform.y };
  context.fillStyle = "#ffffff";
  context.fillRect(0, 0, avatarOutputSize, avatarOutputSize);
  context.drawImage(
    image,
    (avatarOutputSize - transform.drawWidth) / 2 + transform.x,
    (avatarOutputSize - transform.drawHeight) / 2 + transform.y,
    transform.drawWidth,
    transform.drawHeight,
  );
}

function load(): void {
  ready.value = false;
  problem.value = "";
  zoom.value = 1;
  offset.value = { x: 0, y: 0 };
  if (objectUrl.value) URL.revokeObjectURL(objectUrl.value);
  objectUrl.value = URL.createObjectURL(props.file);
  image.onload = () => {
    if (
      image.naturalWidth < 1 ||
      image.naturalHeight < 1 ||
      image.naturalWidth > avatarMaximumDimension ||
      image.naturalHeight > avatarMaximumDimension
    ) {
      problem.value = copy.value.dimensionError;
      return;
    }
    ready.value = true;
    void nextTick(draw);
  };
  image.onerror = () => {
    problem.value = copy.value.processingError;
  };
  image.src = objectUrl.value;
}

function updateZoom(): void {
  draw();
}

function startDrag(event: PointerEvent): void {
  if (!ready.value || props.busy) return;
  dragging.value = true;
  dragStart = {
    clientX: event.clientX,
    clientY: event.clientY,
    offsetX: offset.value.x,
    offsetY: offset.value.y,
  };
  canvas.value?.setPointerCapture(event.pointerId);
}

function moveDrag(event: PointerEvent): void {
  if (!dragging.value || !canvas.value) return;
  const ratio = avatarOutputSize / canvas.value.getBoundingClientRect().width;
  offset.value = {
    x: dragStart.offsetX + (event.clientX - dragStart.clientX) * ratio,
    y: dragStart.offsetY + (event.clientY - dragStart.clientY) * ratio,
  };
  draw();
}

function stopDrag(event: PointerEvent): void {
  dragging.value = false;
  if (canvas.value?.hasPointerCapture(event.pointerId))
    canvas.value.releasePointerCapture(event.pointerId);
}

function moveWithKeyboard(event: KeyboardEvent): void {
  if (!ready.value || props.busy) return;
  const step = event.shiftKey ? 24 : 8;
  const movement: Record<string, AvatarCropOffset> = {
    ArrowLeft: { x: -step, y: 0 },
    ArrowRight: { x: step, y: 0 },
    ArrowUp: { x: 0, y: -step },
    ArrowDown: { x: 0, y: step },
  };
  const delta = movement[event.key];
  if (!delta) return;
  event.preventDefault();
  offset.value = { x: offset.value.x + delta.x, y: offset.value.y + delta.y };
  draw();
}

async function confirm(): Promise<void> {
  if (!canvas.value || !ready.value || props.busy) return;
  draw();
  const blob = await new Promise<Blob | null>((resolve) =>
    canvas.value?.toBlob(resolve, "image/png"),
  );
  if (!blob) {
    problem.value = copy.value.processingError;
    return;
  }
  emit(
    "confirm",
    new File([blob], "agent-avatar.png", {
      type: "image/png",
      lastModified: Date.now(),
    }),
  );
}

watch(() => props.file, load);
onMounted(load);
onBeforeUnmount(() => {
  image.onload = null;
  image.onerror = null;
  if (objectUrl.value) URL.revokeObjectURL(objectUrl.value);
});
</script>

<template>
  <ModalDialog
    :title="copy.cropTitle"
    :busy="busy"
    size="lg"
    @close="emit('close')"
  >
    <div class="avatar-crop">
      <div class="avatar-crop__stage">
        <canvas
          ref="canvas"
          :width="avatarOutputSize"
          :height="avatarOutputSize"
          tabindex="0"
          :aria-label="copy.cropCanvas"
          @pointerdown="startDrag"
          @pointermove="moveDrag"
          @pointerup="stopDrag"
          @pointercancel="stopDrag"
          @keydown="moveWithKeyboard"
        />
        <span class="avatar-crop__frame" aria-hidden="true" />
      </div>
      <div class="avatar-crop__controls">
        <p><Move :size="16" aria-hidden="true" />{{ copy.cropHelp }}</p>
        <label class="field">
          <span>{{ copy.zoom }}</span>
          <input
            v-model.number="zoom"
            type="range"
            min="1"
            max="3"
            step="0.05"
            :disabled="busy || !ready"
            @input="updateZoom"
          />
        </label>
        <p v-if="problem" class="avatar-crop__problem" role="alert">
          {{ problem }}
        </p>
      </div>
    </div>
    <template #actions>
      <button
        class="button"
        type="button"
        :disabled="busy"
        @click="emit('close')"
      >
        {{ $t("common.cancel") }}
      </button>
      <button
        class="button button--primary"
        type="button"
        :disabled="busy || !ready || Boolean(problem)"
        @click="confirm"
      >
        <Crop :size="16" aria-hidden="true" />{{ copy.cropApply }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.avatar-crop {
  display: grid;
  grid-template-columns: minmax(280px, 512px) minmax(220px, 1fr);
  gap: 24px;
  align-items: start;
}
.avatar-crop__stage {
  position: relative;
  width: min(100%, 512px);
  aspect-ratio: 1;
  overflow: hidden;
  border-radius: 8px;
  background: #111827;
}
.avatar-crop__stage canvas {
  display: block;
  width: 100%;
  height: 100%;
  cursor: grab;
  touch-action: none;
}
.avatar-crop__stage canvas:active {
  cursor: grabbing;
}
.avatar-crop__stage canvas:focus-visible {
  outline: 3px solid var(--focus);
  outline-offset: -3px;
}
.avatar-crop__frame {
  position: absolute;
  inset: 0;
  pointer-events: none;
  border: 2px solid rgb(255 255 255 / 88%);
  box-shadow: inset 0 0 0 1px rgb(0 0 0 / 36%);
}
.avatar-crop__controls {
  display: grid;
  gap: 20px;
}
.avatar-crop__controls p {
  display: flex;
  gap: 8px;
  margin: 0;
  color: var(--muted);
  line-height: 1.45;
}
.avatar-crop__problem {
  color: var(--danger) !important;
}
@media (max-width: 760px) {
  .avatar-crop {
    grid-template-columns: 1fr;
  }
}
</style>
