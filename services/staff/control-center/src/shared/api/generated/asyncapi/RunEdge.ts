import {RunEdgeType} from './RunEdgeType';
interface RunEdge {
  ref: string;
  runRef: string;
  sourceNodeRef: string;
  targetNodeRef: string;
  reservedType: RunEdgeType;
  label: string;
}
export { RunEdge };