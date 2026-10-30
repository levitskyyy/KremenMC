package net.minecraft.network.chat.contents;

import net.minecraft.network.chat.ComponentContents;

public record PlainTextContents(String text) implements ComponentContents {}
