import { useState, useCallback, useEffect } from "react";

// Breakpoint constant - extracting magic number
export const MOBILE_BREAKPOINT = 800;

/**
 * Hook that manages mobile view detection based on window size
 * Returns true if window width is <= MOBILE_BREAKPOINT (800px)
 */
export function useIsMobileView(breakpoint = MOBILE_BREAKPOINT) {
  const [mobile, setMobile] = useState(window.innerWidth <= breakpoint);

  const handleWindowSizeChange = useCallback(() => {
    setMobile(window.innerWidth <= breakpoint);
  }, [breakpoint]);

  useEffect(() => {
    window.addEventListener("resize", handleWindowSizeChange);
    return () => {
      window.removeEventListener("resize", handleWindowSizeChange);
    };
  }, [handleWindowSizeChange]);

  return mobile;
}
