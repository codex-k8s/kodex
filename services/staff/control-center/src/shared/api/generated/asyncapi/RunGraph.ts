import {RunNode} from './RunNode';
import {RunEdge} from './RunEdge';
interface RunGraph {
  runRef: string;
  revision: number;
  sequence: number;
  nodes: RunNode[];
  edges: RunEdge[];
}
export { RunGraph };