package net.minecraft.commands.synchronization;

import java.util.HashMap;
import java.util.Map;

/** Brigadier argument types by class; empty, so command frameworks fall back to plain string arguments. */
public final class ArgumentTypeInfos {
    private static final Map<Class<?>, Object> BY_CLASS = new HashMap<>();

    private ArgumentTypeInfos() {}
}
