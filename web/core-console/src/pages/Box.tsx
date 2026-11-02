import { useEffect, useRef } from "react";
import template from "../box/template.html?raw";
import { mountBox } from "../box/controller.js";
import "../box/box.css";

export default function Box() {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (root.current) return mountBox(root.current);
  }, []);
  return (
    <div
      ref={root}
      className="box"
      dangerouslySetInnerHTML={{ __html: template }}
    />
  );
}
