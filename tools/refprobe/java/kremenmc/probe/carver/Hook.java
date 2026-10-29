package kremenmc.probe.carver;

// Hook is the event channel to Jdi: when the probe runs under Jdi every
// ev call is a breakpoint, so its events interleave exactly with the
// observed reference method entries. Standalone it prints directly.
final class Hook {
    static boolean direct = System.getProperty("kremenmc.jdi") == null;

    static void ev(String s) {
        if (direct) Env.out.println(s);
    }
}
