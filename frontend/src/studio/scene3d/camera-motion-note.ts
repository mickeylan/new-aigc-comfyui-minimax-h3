// 从「起幅机位 → 落幅机位」算一句可读的运镜文案。
// 为什么会需要它：首尾帧只锚定两端，中间的运动要靠提示词描述（实测：正文写「环绕半圈」
// 才会真的绕，只给两张图会让中间段被文字/旁白接管）。这里把两个机位的差异翻译成人话，
// 让导出首尾帧的人直接拿去粘进视频提示词。纯函数，不碰 three 与 DOM。

/** 与 CameraKey 同形的只读子集，便于测试里直接写字面量。 */
export interface MotionCamera {
  position: [number, number, number];
  target: [number, number, number];
  fov: number;
}

export interface MotionNoteOptions {
  /** 低于这个角度（度）视为没动，默认 2。 */
  minDegrees?: number;
  /** 低于这个比例（0.03 = 3%）视为距离没变。 */
  minDistanceRatio?: number;
  /** 低于这个米数视为视线中心没动，默认 0.2。 */
  minTargetMeters?: number;
  /** 相机自身位移低于这个米数时改说「原地转向」而不是「环绕」，默认 0.2。 */
  minMoveMeters?: number;
}

const RAD_TO_DEG = 180 / Math.PI;

/** azimuth/elevation 已经返回角度，这里只做四舍五入，别再乘一次。 */
function roundDegrees(value: number) {
  return Math.round(value);
}

/** 水平方位角：+Z 为 0°，+X 为 90°；正增量 = 相机向右绕。 */
function azimuth(position: [number, number, number], target: [number, number, number]) {
  const dx = position[0] - target[0];
  const dz = position[2] - target[2];
  if (Math.hypot(dx, dz) < 1e-6) return 0;
  return Math.atan2(dx, dz) * RAD_TO_DEG;
}

/** 视线朝向：正前方（-Z）为 0°，看向 +X 为正；只在「原地转向」时用。 */
function heading(position: [number, number, number], target: [number, number, number]) {
  const dx = target[0] - position[0];
  const dz = target[2] - position[2];
  if (Math.hypot(dx, dz) < 1e-6) return 0;
  return Math.atan2(dx, -dz) * RAD_TO_DEG;
}

/** 俯仰角：相机高于目标为正。 */
function elevation(position: [number, number, number], target: [number, number, number]) {
  const dy = position[1] - target[1];
  const horizontal = Math.hypot(position[0] - target[0], position[2] - target[2]);
  if (horizontal < 1e-6) return dy >= 0 ? 90 : -90;
  return Math.atan2(dy, horizontal) * RAD_TO_DEG;
}

function distance(position: [number, number, number], target: [number, number, number]) {
  return Math.hypot(
    position[0] - target[0],
    position[1] - target[1],
    position[2] - target[2],
  );
}

/** 把角度差折到 (-180, 180]，避免 350° 被读成「向左绕 350°」。 */
function normalizeDegrees(value: number) {
  let angle = value % 360;
  if (angle > 180) angle -= 360;
  if (angle <= -180) angle += 360;
  return angle;
}

/** 「半圈 / 四分之一圈」这类说法只在大致对得上时才用，免得把 137° 说成半圈。 */
function turnPhrase(degreesValue: number) {
  const size = Math.abs(degreesValue);
  if (Math.abs(size - 360) <= 8) return "（一圈）";
  if (Math.abs(size - 180) <= 8) return "（半圈）";
  if (Math.abs(size - 90) <= 6) return "（四分之一圈）";
  return "";
}

/**
 * 生成一句运镜文案；机位差都小于阈值时返回「机位基本不动」。
 * 两个机位以上请用 firstLastMotionNote，本函数只吃起幅与落幅。
 */
export function cameraMotionNote(
  first: MotionCamera | null | undefined,
  last: MotionCamera | null | undefined,
  options: MotionNoteOptions = {},
) {
  if (!first || !last) return "";
  const minDegrees = options.minDegrees ?? 2;
  const minDistanceRatio = options.minDistanceRatio ?? 0.03;
  const minTargetMeters = options.minTargetMeters ?? 0.2;
  const minMoveMeters = options.minMoveMeters ?? 0.2;

  const parts: string[] = [];

  const cameraShift = Math.hypot(
    last.position[0] - first.position[0],
    last.position[1] - first.position[1],
    last.position[2] - first.position[2],
  );

  if (cameraShift < minMoveMeters) {
    // 机位没挪、只是镜头转了：这是摇镜。用方位角报会算成假的「绕圈」。
    const deltaHeading = normalizeDegrees(heading(last.position, last.target) - heading(first.position, first.target));
    if (Math.abs(deltaHeading) >= minDegrees) {
      const direction = deltaHeading > 0 ? "向右" : "向左";
      parts.push(`机位原地${direction}转向 ${roundDegrees(Math.abs(deltaHeading))}°`);
    }
  } else {
    const deltaAzimuth = normalizeDegrees(azimuth(last.position, last.target) - azimuth(first.position, first.target));
    if (Math.abs(deltaAzimuth) >= minDegrees) {
      const direction = deltaAzimuth > 0 ? "向右" : "向左";
      parts.push(`相机${direction}环绕 ${roundDegrees(Math.abs(deltaAzimuth))}°${turnPhrase(deltaAzimuth)}`);
    }
  }

  const deltaElevation = elevation(last.position, last.target) - elevation(first.position, first.target);
  if (Math.abs(deltaElevation) >= minDegrees) {
    parts.push(
      deltaElevation > 0
        ? `机位抬高 ${roundDegrees(deltaElevation)}°`
        : `机位降低 ${roundDegrees(Math.abs(deltaElevation))}°`,
    );
  }

  const firstDistance = distance(first.position, first.target);
  const lastDistance = distance(last.position, last.target);
  if (firstDistance > 1e-6) {
    const ratio = lastDistance / firstDistance;
    if (Math.abs(ratio - 1) >= minDistanceRatio) {
      const percent = Math.round(Math.abs(ratio - 1) * 100);
      parts.push(ratio < 1 ? `距离推近 ${percent}%` : `距离拉远 ${percent}%`);
    }
  }

  const deltaFov = last.fov - first.fov;
  if (Math.abs(deltaFov) >= 1) {
    parts.push(`视角从 ${Math.round(first.fov)}° 收到 ${Math.round(last.fov)}°`);
  }

  const targetShift = Math.hypot(
    last.target[0] - first.target[0],
    last.target[1] - first.target[1],
    last.target[2] - first.target[2],
  );
  if (targetShift >= minTargetMeters) {
    parts.push(`视线中心平移 ${targetShift.toFixed(1)} 米`);
  }

  if (parts.length === 0) return "机位基本不动（只差在阈值以内）";
  return parts.join("；");
}

/** 首尾帧用的快捷入口：取第一个与最后一个机位，其余机位不参与。 */
export function firstLastMotionNote(keys: readonly MotionCamera[] | null | undefined, options: MotionNoteOptions = {}) {
  if (!Array.isArray(keys) || keys.length < 2) return "";
  return cameraMotionNote(keys[0], keys[keys.length - 1], options);
}