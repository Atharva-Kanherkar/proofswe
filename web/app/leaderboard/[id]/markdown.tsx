"use client";

import { memo } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";

type AnchorProps = React.AnchorHTMLAttributes<HTMLAnchorElement>;

// Markdown renders assistant/developer prose: GFM (tables, lists, strikethrough)
// plus syntax-highlighted fenced code blocks. Links open in a new tab.
function MarkdownImpl({ children }: { children: string }) {
  return (
    <div className="md">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[[rehypeHighlight, { detect: true, ignoreMissing: true }]]}
        components={{
          a: ({ href, ...props }: AnchorProps) => (
            <a href={href} target="_blank" rel="noopener noreferrer" {...props} />
          ),
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  );
}

const Markdown = memo(MarkdownImpl);
export default Markdown;
