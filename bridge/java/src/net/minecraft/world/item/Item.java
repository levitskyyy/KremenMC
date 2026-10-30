package net.minecraft.world.item;

import net.minecraft.world.level.ItemLike;

public class Item implements ItemLike {
    @Override public Item asItem() { return this; }
}
